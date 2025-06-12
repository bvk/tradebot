// Copyright (c) 2023 BVK Chaitanya

package limiter

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/bvk/tradebot/exchange"
	"github.com/bvk/tradebot/trader"
	"github.com/bvkgo/kv"
	"github.com/visvasity/topic"
)

func (v *Limiter) Run(ctx context.Context, rt *trader.Runtime) error {
	v.runtimeLock.Lock()
	defer v.runtimeLock.Unlock()

	log.Printf("%s:%s: started limiter job", v.uid, v.point)
	if rt.Product.ProductID() != v.productID {
		return os.ErrInvalid
	}
	// We also need to handle resume logic here.
	nupdated, err := v.fetchOrderMap(ctx, rt.Product)
	if err != nil {
		log.Printf("%s:%s: could not refresh/fetch order map: %v", v.uid, v.point, err)
		return err
	}

	if p := v.PendingSize(); p.IsZero() {
		if nupdated != 0 {
			_ = kv.WithReadWriter(ctx, rt.Database, v.Save)
		}
		asyncUpdateFinishTime(v)
		log.Printf("%s:%s: limiter is complete cause pending size is zero", v.uid, v.point)
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
		log.Printf("%s:%s: found %d live orders in the order map", v.uid, v.point, nlive)
		return fmt.Errorf("found %d live orders (want 0 or 1)", nlive)
	}

	var activeOrderID string
	if nlive != 0 {
		activeOrderID = live[0].ServerOrderID
		log.Printf("%s:%s: reusing existing order %s as the active order", v.uid, v.point, activeOrderID)
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
				log.Printf("%s:%s: canceling active limit order %v (%v)", v.uid, v.point, activeOrderID, context.Cause(ctx))
				if err := v.cancel(localCtx, rt.Product, activeOrderID); err != nil {
					return err
				}
				dirty++
			}
			if err := kv.WithReadWriter(localCtx, rt.Database, v.Save); err != nil {
				log.Printf("%s:%s dirty limit order state could not be saved to the database (will retry): %v", v.uid, v.point, err)
			}
			asyncUpdateFinishTime(v)
			return context.Cause(ctx)

		case <-flushCh:
			if dirty > 0 {
				if err := kv.WithReadWriter(ctx, rt.Database, v.Save); err != nil {
					log.Printf("%s:%s dirty limit order state could not be saved to the database (will retry): %v", v.uid, v.point, err)
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
				log.Printf("%s:%s: limit order with server order-id %s is completed with status %q (DoneReason %q)", v.uid, v.point, activeOrderID, order.Status, order.DoneReason)
				activeOrderID = ""
			}

		case ticker := <-tickerCh:
			tickerPrice, _ := ticker.PricePoint()

			if v.IsSell() {
				if tickerPrice.LessThanOrEqual(v.point.Cancel) {
					if activeOrderID != "" {
						if err := v.cancel(localCtx, rt.Product, activeOrderID); err != nil {
							return err
						}
						dirty++
						activeOrderID = ""
					}
				}
				if tickerPrice.GreaterThan(v.point.Cancel) {
					if activeOrderID == "" {
						id, err := v.create(localCtx, rt.Product)
						if err != nil {
							return err
						}
						dirty++
						activeOrderID = id
					}
				}
				continue
			}

			if v.IsBuy() {
				if tickerPrice.GreaterThanOrEqual(v.point.Cancel) {
					if activeOrderID != "" {
						if err := v.cancel(localCtx, rt.Product, activeOrderID); err != nil {
							return err
						}
						dirty++
						activeOrderID = ""
					}
				}
				if tickerPrice.GreaterThanOrEqual(v.point.Price) && tickerPrice.LessThan(v.point.Cancel) {
					if activeOrderID == "" {
						id, err := v.create(localCtx, rt.Product)
						if err != nil {
							return err
						}
						dirty++
						activeOrderID = id
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

func (v *Limiter) create(ctx context.Context, product exchange.Product) (string, error) {
	offset := v.idgen.Offset()
	clientOrderID := v.idgen.NextID()

	size := v.PendingSize()
	if size.LessThan(product.BaseMinSize()) {
		size = product.BaseMinSize()
	}

	var err error
	var latency time.Duration
	var order exchange.Order
	if v.IsSell() {
		s := time.Now()
		order, err = product.LimitSell(ctx, clientOrderID, size, v.point.Price)
		latency = time.Now().Sub(s)
	} else {
		s := time.Now()
		order, err = product.LimitBuy(ctx, clientOrderID, size, v.point.Price)
		latency = time.Now().Sub(s)
	}
	if err != nil {
		v.idgen.RevertID()
		log.Printf("%s:%s: create limit order with client-order-id %s (%d reverted) has failed (in %s): %v", v.uid, v.point, clientOrderID, offset, latency, err)
		return "", err
	}

	orderID := order.ServerID()
	sorder, err := exchange.NewSimpleOrder(orderID, order.ClientID(), order.OrderSide())
	if err != nil {
		return "", err
	}
	v.orderMap.Store(orderID, sorder)
	log.Printf("%s:%s: created a new limit order %s with client-order-id %s (%d) in %s", v.uid, v.point, orderID, clientOrderID, offset, latency)
	return orderID, nil
}

func (v *Limiter) cancel(ctx context.Context, product exchange.Product, activeOrderID string) error {
	if err := product.Cancel(ctx, activeOrderID); err != nil {
		log.Printf("%s:%s: cancel limit order %s has failed: %v", v.uid, v.point, activeOrderID, err)
		return err
	}
	// log.Printf("%s:%s: canceled the limit order %s", v.uid, v.point, activeOrderID)
	return nil
}

func (v *Limiter) fetchOrderMap(ctx context.Context, product exchange.Product) (nupdated int, status error) {
	for id, order := range v.dupOrderMap() {
		if order.Done {
			continue
		}
		detail, err := product.Get(ctx, id)
		if err != nil {
			log.Printf("%s:%s: could not fetch order with id %s: %v", v.uid, v.point, id, err)
			return nupdated, err
		}
		sorder, err := exchange.SimpleOrderFromOrderDetail(detail)
		if err != nil {
			return nupdated, err
		}
		v.orderMap.Store(id, sorder)
		nupdated++
	}
	return nupdated, nil
}
