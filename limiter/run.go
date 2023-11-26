// Copyright (c) 2023 BVK Chaitanya

package limiter

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/bvk/tradebot/exchange"
	"github.com/bvk/tradebot/idgen"
	"github.com/bvk/tradebot/runtime"
	"github.com/bvkgo/kv"
)

func (v *Limiter) Run(ctx context.Context, rt *runtime.Runtime) error {
	if rt.Product.ProductID() != v.productID {
		return os.ErrInvalid
	}
	// We also need to handle resume logic here.
	if err := v.fetchOrderMap(ctx, rt.Product, len(v.orderMap)); err != nil {
		return err
	}
	if p := v.Pending(); p.IsZero() {
		return nil
	}

	// Check if any of the orders in the orderMap are still active on the
	// exchange.
	var live []*exchange.Order
	for _, order := range v.orderMap {
		if !order.Done {
			live = append(live, order)
		}
	}
	nlive := len(live)
	if nlive > 1 {
		return fmt.Errorf("found %d live orders (want 0 or 1)", nlive)
	}

	var activeOrderID exchange.OrderID
	var orderUpdatesCh <-chan *exchange.Order
	if nlive != 0 {
		activeOrderID = live[0].OrderID
		orderUpdatesCh = rt.Product.OrderUpdatesCh(activeOrderID)
		log.Printf("%s:%s: reusing existing order %s as the active order", v.uid, v.point, activeOrderID)
	}

	dirty := 0
	tickerCh := rt.Product.TickerCh()
	flushCh := time.After(time.Minute)

	localCtx := context.Background()
	for p := v.Pending(); !p.IsZero(); p = v.Pending() {
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
				log.Printf("%s:%s dirty limit %s state could not be saved to the database (will retry): %v", v.uid, v.point, v.Side(), err)
			}
			return context.Cause(ctx)

		case <-flushCh:
			if dirty > 0 {
				if err := kv.WithReadWriter(ctx, rt.Database, v.Save); err != nil {
					log.Printf("%s:%s dirty limit %s state could not be saved to the database (will retry): %v", v.uid, v.point, v.Side(), err)
				} else {
					dirty = 0
				}
			}
			flushCh = time.After(time.Minute)

		case order := <-orderUpdatesCh:
			dirty++
			if err := v.updateOrderMap(order); err != nil {
				return err
			}
			if order.Done && order.OrderID == activeOrderID {
				log.Printf("%s:%s: limit %s order with server order-id %s is completed with status %q (DoneReason %q)", v.uid, v.point, v.Side(), activeOrderID, order.Status, order.DoneReason)
				activeOrderID = ""
			}

		case ticker := <-tickerCh:
			if v.Side() == "SELL" {
				if ticker.Price.LessThanOrEqual(v.point.Cancel) {
					if activeOrderID != "" {
						if err := v.cancel(localCtx, rt.Product, activeOrderID); err != nil {
							return err
						}
						dirty++
						activeOrderID, orderUpdatesCh = "", nil
					}
				}
				if ticker.Price.GreaterThan(v.point.Cancel) {
					if activeOrderID == "" {
						id, ch, err := v.create(localCtx, rt.Product)
						if err != nil {
							return err
						}
						dirty++
						activeOrderID, orderUpdatesCh = id, ch
					}
				}
				continue
			}

			if v.Side() == "BUY" {
				if ticker.Price.GreaterThanOrEqual(v.point.Cancel) {
					if activeOrderID != "" {
						if err := v.cancel(localCtx, rt.Product, activeOrderID); err != nil {
							return err
						}
						dirty++
						activeOrderID, orderUpdatesCh = "", nil
					}
				}
				if ticker.Price.LessThan(v.point.Cancel) {
					if activeOrderID == "" {
						id, ch, err := v.create(localCtx, rt.Product)
						if err != nil {
							return err
						}
						dirty++
						activeOrderID, orderUpdatesCh = id, ch
					}
				}
				continue
			}
		}
	}

	if err := v.fetchOrderMap(ctx, rt.Product, len(v.orderMap)); err != nil {
		return err
	}
	if err := kv.WithReadWriter(ctx, rt.Database, v.Save); err != nil {
		return err
	}
	return nil
}

// Fix is a temporary helper interface used to fix any past mistakes.
func (v *Limiter) Fix(ctx context.Context, product exchange.Product, db kv.Database) error {
	v.idgen = idgen.New(v.idgen.Seed(), v.idgen.Offset()+1000000)
	return nil
}

func (v *Limiter) create(ctx context.Context, product exchange.Product) (exchange.OrderID, <-chan *exchange.Order, error) {
	offset := v.idgen.Offset()
	clientOrderID := v.idgen.NextID()
	size := v.Pending()
	if size.LessThan(product.BaseMinSize()) {
		size = product.BaseMinSize()
	}

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
		log.Printf("%s:%s: create limit %s order with client-order-id %s (%d reverted) has failed (in %s): %v", v.uid, v.point, v.Side(), clientOrderID, offset, latency, err)
		return "", nil, err
	}

	v.orderMap[orderID] = &exchange.Order{
		OrderID:       orderID,
		ClientOrderID: clientOrderID.String(),
		Side:          v.Side(),
	}
	v.clientServerMap[clientOrderID.String()] = orderID

	log.Printf("%s:%s: created a new limit %s order %s with client-order-id %s (%d) in %s", v.uid, v.point, v.Side(), orderID, clientOrderID, offset, latency)
	return orderID, product.OrderUpdatesCh(orderID), nil
}

func (v *Limiter) cancel(ctx context.Context, product exchange.Product, activeOrderID exchange.OrderID) error {
	if err := product.Cancel(ctx, activeOrderID); err != nil {
		log.Printf("%s:%s: cancel limit %s order %s has failed: %v", v.uid, v.point, v.Side(), activeOrderID, err)
		return err
	}
	log.Printf("%s:%s: canceled the limit %s order %s", v.uid, v.point, v.Side(), activeOrderID)
	return nil
}

func (v *Limiter) fetchOrderMap(ctx context.Context, product exchange.Product, n int) error {
	for id, order := range v.orderMap {
		if order.Done {
			continue
		}
		order, err := product.Get(ctx, id)
		if err != nil {
			return err
		}
		v.orderMap[id] = order
		if n--; n <= 0 {
			break
		}
	}
	return nil
}
