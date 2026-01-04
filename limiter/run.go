// Copyright (c) 2023 BVK Chaitanya

package limiter

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/bvk/tradebot/exchange"
	"github.com/bvk/tradebot/trader"
	"github.com/bvkgo/kv"
	"github.com/visvasity/topic"
)

const SaveClientIDOffsetSize = 10
const CancelOffsetTimeout = time.Minute

func (v *Limiter) Run(ctx context.Context, rt *trader.Runtime) error {
	v.runtimeLock.Lock()
	defer v.runtimeLock.Unlock()

	slog.Info("started limiter job", "limiter", v, "point", v.point)
	if rt.Product.ProductID() != v.productID {
		return os.ErrInvalid
	}
	// We also need to handle resume logic here.
	nupdated, err := v.fetchOrderMap(ctx, rt.Product)
	if err != nil {
		slog.Error("could not refresh/fetch order map", "limiter", v, "err", err)
		return err
	}

	if p := v.PendingSize(); p.IsZero() {
		if nupdated != 0 {
			_ = kv.WithReadWriter(ctx, rt.Database, v.Save)
		}
		asyncUpdateFinishTime(v)
		slog.Info("limiter is complete cause pending size is zero", "limiter", v, "point", v.point)
		return nil
	}

	// Check if any of the orders in the orderMap are still active on the
	// exchange.
	var live []*exchange.SimpleOrder
	for _, order := range v.dupOrderMap() {
		if !order.Done {
			live = append(live, order)
		}
	}

	nlive := len(live)
	if nlive > 1 {
		slog.Error("found unexpected number of live orders (0 or 1 are expected)", "limiter", v, "point", v.point, "nlive", nlive)
		return fmt.Errorf("found %d live orders (want 0 or 1)", nlive)
	}

	var activeOrderID string
	var activeOrderAt time.Time
	if nlive != 0 {
		activeOrderID = live[0].ServerOrderID
		activeOrderAt = live[0].CreateTime.Time
		slog.Warn("reusing existing order as the active order", "limiter", v, "point", v.point, "order-id", activeOrderID)
	}

	dirty := 0
	flushCh := time.After(time.Minute)

	localCtx := context.Background()

	priceUpdates, err := rt.Product.GetPriceUpdates()
	if err != nil {
		return err
	}
	defer priceUpdates.Close()

	tickerCh, err := topic.ReceiveCh(priceUpdates)
	if err != nil {
		return err
	}

	orderUpdates, err := rt.Product.GetOrderUpdates()
	if err != nil {
		return err
	}
	defer orderUpdates.Close()

	orderUpdatesCh, err := topic.ReceiveCh(orderUpdates)
	if err != nil {
		return err
	}

	for p := v.PendingSize(); !p.IsZero(); p = v.PendingSize() {
		select {
		case <-ctx.Done():
			if activeOrderID != "" {
				slog.Info("canceling active limit order before quitting", "limiter", v, "point", v.point, "order-id", activeOrderID, "quit-reason", context.Cause(ctx))
				if err := v.cancel(localCtx, rt.Product, activeOrderID); err != nil {
					return fmt.Errorf("could not cancel order %v: %w", activeOrderID, err)
				}
				slog.Info("cancelled active limit order before quitting", "limiter", v, "point", v.point, "order-id", activeOrderID, "quit-reason", context.Cause(ctx))
				dirty++
			}
			if err := kv.WithReadWriter(localCtx, rt.Database, v.Save); err != nil {
				slog.Error("dirty limiter state could not be saved to the database before quitting (ignored)", "limiter", v, "point", v.point, "err", err)
			}
			asyncUpdateFinishTime(v)
			return context.Cause(ctx)

		case <-flushCh:
			if dirty > 0 {
				if err := kv.WithReadWriter(ctx, rt.Database, v.Save); err != nil {
					slog.Error("dirty limiter state could not be saved (will retry)", "limiter", v, "point", v.point, "err", err)
				} else {
					dirty = 0
				}
			}
			flushCh = time.After(time.Minute)

		case update := <-orderUpdatesCh:
			dirty++
			order, err := v.updateOrderMap(update)
			if err != nil {
				if !errors.Is(err, os.ErrNotExist) {
					return err
				}
			}
			if order != nil && order.IsDone() && order.ServerOrderID == activeOrderID {
				slog.Info("limiter order is complete", "limiter", v, "point", v.point, "order-id", activeOrderID, "order-status", order.Status, "done-reason", order.DoneReason)
				activeOrderID = ""
			}

		case ticker := <-tickerCh:
			now := time.Now()
			tickerPrice, _ := ticker.PricePoint()

			if v.IsSell() {
				if tickerPrice.LessThanOrEqual(v.point.Cancel) {
					// Cancel an order after a minute has passed and cancel price is
					// breached. One minute timeout reduces number of overall exchange
					// operations.
					if activeOrderID != "" && activeOrderAt.Add(CancelOffsetTimeout).Before(now) {
						if err := v.cancel(localCtx, rt.Product, activeOrderID); err != nil {
							return err
						}
						dirty++
						activeOrderID = ""
						activeOrderAt = time.Time{}
					}
				}
				if tickerPrice.GreaterThan(v.point.Cancel) {
					if activeOrderID == "" {
						id, err := v.create(localCtx, rt)
						if err != nil {
							return err
						}
						dirty++
						activeOrderID = id
						activeOrderAt = now
					}
				}
				continue
			}

			if v.IsBuy() {
				if tickerPrice.GreaterThanOrEqual(v.point.Cancel) {
					// Cancel an order after a minute has passed and cancel price is
					// breached. One minute timeout reduces number of overall exchange
					// operations.
					if activeOrderID != "" && activeOrderAt.Add(CancelOffsetTimeout).Before(now) {
						if err := v.cancel(localCtx, rt.Product, activeOrderID); err != nil {
							return err
						}
						dirty++
						activeOrderID = ""
						activeOrderAt = time.Time{}
					}
				}
				if tickerPrice.GreaterThanOrEqual(v.point.Price) && tickerPrice.LessThan(v.point.Cancel) {
					if activeOrderID == "" {
						id, err := v.create(localCtx, rt)
						if err != nil {
							return err
						}
						dirty++
						activeOrderID = id
						activeOrderAt = now
					}
				}
				continue
			}
		}
	}

	if _, err := v.fetchOrderMap(ctx, rt.Product); err != nil {
		return err
	}
	if err := kv.WithReadWriter(ctx, rt.Database, v.Save); err != nil {
		return err
	}
	asyncUpdateFinishTime(v)
	return nil
}

// Fix is a temporary helper interface used to fix any past mistakes.
func (v *Limiter) Fix(ctx context.Context, rt *trader.Runtime) error {
	v.runtimeLock.Lock()
	defer v.runtimeLock.Unlock()

	return nil
}

func (v *Limiter) Refresh(ctx context.Context, rt *trader.Runtime) error {
	v.runtimeLock.Lock()
	defer v.runtimeLock.Unlock()

	if _, err := v.fetchOrderMap(ctx, rt.Product); err != nil {
		return fmt.Errorf("could not refresh limiter state: %w", err)
	}
	// FIXME: We may also need to check for presence of unsaved orders with future client-ids.
	return nil
}

func (v *Limiter) create(ctx context.Context, rt *trader.Runtime) (string, error) {
	offset := v.idgen.Offset()
	clientOrderID := v.idgen.NextID()

	if v.idgen.Offset()%SaveClientIDOffsetSize == 0 {
		if err := kv.WithReadWriter(ctx, rt.Database, v.Save); err != nil {
			slog.Error("limiter state could not be saved (will retry)", "limiter", v, "point", v.point, "err", err)
			return "", err
		}
	}

	size := v.PendingSize()
	if size.LessThan(rt.Product.BaseMinSize()) {
		size = rt.Product.BaseMinSize()
	}

	var err error
	var latency time.Duration
	var order exchange.Order
	if v.IsSell() {
		s := time.Now()
		order, err = rt.Product.LimitSell(ctx, clientOrderID, size, v.point.Price)
		latency = time.Now().Sub(s)
	} else {
		s := time.Now()
		order, err = rt.Product.LimitBuy(ctx, clientOrderID, size, v.point.Price)
		latency = time.Now().Sub(s)
	}
	if err != nil {
		if rt.Exchange.CanDedupOnClientUUID() {
			v.idgen.RevertID()
			slog.Error("create limit order has failed; client order id is reverted", "limiter", v, "point", v.point, "client-order-id", clientOrderID, "offset", offset, "latency", latency, "err", err)
			return "", err
		}
		slog.Error("create limit order has failed", "limiter", v, "point", v.point, "client-order-id", clientOrderID, "latency", latency, "err", err)
		return "", err
	}

	orderID := order.ServerID()
	sorder, err := exchange.NewSimpleOrder(orderID, order.ClientID(), order.OrderSide())
	if err != nil {
		return "", err
	}
	v.orderMap.Store(orderID, sorder)
	slog.Info("created new limit order", "limiter", v, "point", v.point, "order-id", orderID, "client-order-id", clientOrderID, "offset", offset, "latency", latency)
	return orderID, nil
}

func (v *Limiter) cancel(ctx context.Context, product exchange.Product, activeOrderID string) error {
	if err := product.Cancel(ctx, activeOrderID); err != nil {
		slog.Error("cancel limit order has failed", "limiter", v, "point", v.point, "order-id", activeOrderID, "err", err)
		return err
	}
	for {
		detail, err := product.Get(ctx, activeOrderID)
		if err != nil {
			slog.Warn("could not fetch canceled order (will retry)", "order-id", activeOrderID, "err", err)
			time.Sleep(time.Second)
			continue
		}
		if !detail.IsDone() {
			slog.Warn("canceled order is still not done (will retry)", "order-id", activeOrderID)
			time.Sleep(time.Second)
			continue
		}
		if _, err := v.updateOrderMap(detail); err != nil {
			slog.Error("could not update limiter state with canceled order update", "limiter", v, "point", v.point, "order-id", activeOrderID, "err", err)
			return err
		}
		return nil
	}
}

func (v *Limiter) fetchOrderMap(ctx context.Context, product exchange.Product) (nupdated int, status error) {
	for id, order := range v.dupOrderMap() {
		if order.IsDone() {
			continue
		}
		detail, err := product.Get(ctx, id)
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				slog.Error("could not fetch limit order", "limiter", v, "point", v.point, "order-id", id, "err", err)
				return nupdated, err
			}
			// Exchanges may not keep the cancelled orders with no executed
			// value. So, assign canceled status to non-existing orders.
			order.Done = true
			order.DoneReason = "NOTFOUND/CANCELED"
			continue
		}

		sorder, err := exchange.NewSimpleOrderFromOrderDetail(detail)
		if err != nil {
			return nupdated, err
		}
		v.orderMap.Store(id, sorder)
		nupdated++
	}
	return nupdated, nil
}
