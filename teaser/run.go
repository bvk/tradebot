// Copyright (c) 2025 BVK Chaitanya

package teaser

import (
	"context"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"os"
	"time"

	"github.com/bvk/tradebot/exchange"
	"github.com/bvk/tradebot/trader"
	"github.com/bvkgo/kv"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/visvasity/topic"
)

var minQuoteSize = decimal.NewFromFloat(0.01) // One cent.

func (v *Teaser) Run(ctx context.Context, rt *trader.Runtime) error {
	if !v.runtimeLock.TryLock() {
		return fmt.Errorf("already running: %w", os.ErrInvalid)
	}
	defer v.runtimeLock.Unlock()

	if rt.Product.ProductID() != v.product {
		return os.ErrInvalid
	}

	// Resolve existing orders.
	if err := v.resolveExistingOrders(ctx, rt.Product); err != nil {
		return err
	}

	// Recompute summary if it isn't already available.
	if v.summary.Load() == nil {
		if err := kv.WithReadWriter(ctx, rt.Database, v.Save); err != nil {
			return err
		}
	}

	// Prepare ticker price updates channel.
	priceUpdates, err := rt.Product.GetPriceUpdates()
	if err != nil {
		return err
	}
	defer priceUpdates.Close()

	tickerCh, err := topic.ReceiveCh(priceUpdates)
	if err != nil {
		return err
	}

	// Prepare order updates channel.
	orderUpdates, err := rt.Product.GetOrderUpdates()
	if err != nil {
		return err
	}
	defer orderUpdates.Close()

	orderUpdatesCh, err := topic.ReceiveCh(orderUpdates)
	if err != nil {
		return err
	}

	ndirty := 0
	localCtx := context.Background()
	flushCh := time.After(time.Minute)

	for {
		select {
		case <-ctx.Done():
			if err := kv.WithReadWriter(localCtx, rt.Database, v.Save); err != nil {
				slog.Error("could not save teaser state before stopping the run", "teaser", v, "err", err)
				return err
			}
			return context.Cause(ctx)

		case <-flushCh:
			if ndirty > 0 {
				if err := kv.WithReadWriter(ctx, rt.Database, v.Save); err != nil {
					slog.Error("could not save teaser state after flush interval (will retry)", "teaser", v, "ndirty", ndirty, "err", err)
				} else {
					ndirty = 0
				}
			}
			flushCh = time.After(time.Minute)

		case order := <-orderUpdatesCh:
			for _, loop := range v.loops {
				dirty, err := v.handleOrderUpdate(ctx, rt, loop, order)
				if dirty {
					ndirty++
				}
				if err != nil {
					slog.Error("could not handle order update for loop (ignored)", "loop", loop, "err", err)
				}
			}

		case ticker := <-tickerCh:
			// Process the loops in closest-to-ticker first order.
			price, _ := ticker.PricePoint()
			for _, loop := range v.orderLoopsByPrice(price) {
				dirty, err := v.handlePriceUpdate(ctx, rt, loop, ticker)
				if dirty {
					ndirty++
				}
				if err != nil {
					slog.Error("could not handle price update for loop (ignored)", "loop", loop, "err", err)
				}
			}
		}
	}
}

func (v *Teaser) handleOrderUpdate(ctx context.Context, rt *trader.Runtime, loop *teaserLoop, update exchange.OrderUpdate) (dirty bool, err error) {
	if loop.order == nil {
		return false, nil
	}
	if loop.order.ServerOrderID != update.ServerID() {
		return false, nil
	}
	if _, err := loop.order.AddUpdate(update); err != nil {
		slog.Error("could not handle order update for an order (ignored)", "err", err)
		return false, err
	}
	loop.refresh()
	return true, nil
}

func (v *Teaser) handlePriceUpdate(ctx context.Context, rt *trader.Runtime, loop *teaserLoop, ticker exchange.PriceUpdate) (dirty bool, err error) {
	if loop.order != nil {
		if loop.order.Side == "BUY" {
			return v.teaseBuy(ctx, rt, loop, ticker)
		}
		if loop.order.Side == "SELL" {
			return v.teaseSell(ctx, rt, loop, ticker)
		}
		slog.Error("unexpected order side", "side", loop.order.Side, "loop", loop, "teaser", v)
		return false, os.ErrInvalid
	}

	clientOrderID := makeClientOrderID(v.uid, loop.data.Pair.Buy.Price.StringFixed(2), loop.data.Pair.Sell.Price.StringFixed(2), loop.data.ClientIDOffset)

	minBaseSize := rt.Product.BaseMinSize()
	switch action := loop.nextAction(minBaseSize, minQuoteSize); action {
	default:
		slog.Error("loop instance is stopped due to invalid action", "teaser", v, "loop", loop, "action", action)
		return false, os.ErrInvalid

	case "BUY":
		_, pending := loop.bsize.QuoRem(loop.data.Pair.Buy.Size, int32(decimal.DivisionPrecision))
		if pending.LessThan(minBaseSize) {
			pending = minBaseSize
		}
		// TODO: Create buy order for the pending size.
		s := time.Now()
		order, err := rt.Product.LimitSell(ctx, clientOrderID, pending, loop.data.Pair.Buy.Price)
		latency := time.Since(s)
		if err != nil {
			return err
		}
		orderID := order.ServerID()
		slog.Info("created a new buy order", "teaser", v, "loop", loop, "point", loop.data.Pair.Buy, "order-id", orderID, "latency", latency)
		sorder, err := exchange.NewSimpleOrder(orderID, order.ClientID(), order.OrderSide())
		if err != nil {
			return err
		}
		loop.order = sorder
		loop.refresh()

	case "SELL":
		_, pending := loop.ssize.QuoRem(loop.data.Pair.Sell.Size, int32(decimal.DivisionPrecision))
		if pending.LessThan(minBaseSize) {
			pending = minBaseSize
		}
		// TODO: Create sell order for the pending size.
	}

	return nil
}

func (v *Teaser) teaseBuy(ctx context.Context, rt *trader.Runtime, loop *teaserLoop, ticker exchange.PriceUpdate) (dirty bool, err error) {
	return false, nil
}

func (v *Teaser) teaseSell(ctx context.Context, rt *trader.Runtime, loop *teaserLoop, ticker exchange.PriceUpdate) (dirty bool, err error) {
	return false, nil
}

func (v *Teaser) orderLoopsByPrice(price decimal.Decimal) []*teaserLoop {
	return nil
}

func (v *Teaser) create(ctx context.Context, product exchange.Product, ticker *exchange.Ticker) (exchange.OrderID, decimal.Decimal, <-chan *exchange.Order, error) {
	clientOrderID := v.idgen.NextID()
	size := v.Pending()

	var err error
	var latency time.Duration
	var orderID exchange.OrderID
	if v.Side() == "SELL" {
		s := time.Now()
		orderID, err = product.LimitSell(ctx, clientOrderID.String(), size, v.point.Price)
		latency = time.Now().Sub(s)
	} else {
		s := time.Now()
		orderID, err = product.LimitBuy(ctx, clientOrderID.String(), size, v.point.Price)
		latency = time.Now().Sub(s)
	}
	if err != nil {
		v.idgen.RevertID()
		log.Printf("%s:%s: create limit %s order with client-order-id %s has failed (in %s): %v", v.uid, v.point, v.Side(), clientOrderID, latency, err)
		return "", nil, err
	}

	v.orderMap[orderID] = &exchange.Order{
		OrderID:       orderID,
		ClientOrderID: clientOrderID.String(),
		Side:          v.Side(),
	}
	v.clientServerMap[clientOrderID.String()] = orderID

	log.Printf("%s:%s: created a new limit %s order %s with client-order-id %s in %s", v.uid, v.point, v.Side(), orderID, clientOrderID, latency)
	return orderID, product.OrderUpdatesCh(orderID), nil
}

func (v *Teaser) cancel(ctx context.Context, product exchange.Product, activeOrderID exchange.OrderID) error {
	if err := product.Cancel(ctx, activeOrderID); err != nil {
		log.Printf("%s:%s: cancel limit %s order %s has failed: %v", v.uid, v.point, v.Side(), activeOrderID, err)
		return err
	}
	log.Printf("%s:%s: canceled the limit %s order %s", v.uid, v.point, v.Side(), activeOrderID)
	return nil
}

func (v *Teaser) compactOrderMap() {
	for id, order := range v.orderMap {
		if order.Done && order.FilledSize.IsZero() {
			delete(v.orderMap, id)
			continue
		}
	}
}

func (v *Teaser) updateOrderMap(order *exchange.Order) error {
	if _, ok := v.orderMap[order.OrderID]; !ok {
		return nil
	}
	v.orderMap[order.OrderID] = order
	return nil
}

func (v *Teaser) resolveExistingOrders(ctx context.Context, product exchange.Product) error {
	return errors.New("TODO")
}

func makeClientOrderID(uid string, buyPrice, sellPrice string, offset int64) (id uuid.UUID) {
	return id
}
