// Copyright (c) 2023 BVK Chaitanya

package limiter

import (
	"bytes"
	"context"
	"encoding/gob"
	"fmt"
	"log"
	"path"

	"github.com/bvkgo/kv"
	"github.com/bvkgo/tradebot/exchange"
	"github.com/bvkgo/tradebot/kvutil"
	"github.com/bvkgo/tradebot/point"
	"github.com/shopspring/decimal"
)

const DefaultKeyspace = "/limiters"

type Limiter struct {
	product exchange.Product

	key string

	point point.Point

	idgen *idGenerator

	tickerCh <-chan *exchange.Ticker

	orderUpdatesCh <-chan *exchange.Order

	activeOrderID exchange.OrderID

	orderMap map[exchange.OrderID]*exchange.Order
}

type State struct {
	ProductID string
	Offset    uint64
	Point     point.Point
	OrderMap  map[exchange.OrderID]*exchange.Order
}

type Status struct {
	UID string

	ProductID string

	Side string

	Point point.Point

	Pending decimal.Decimal
}

// New creates a new BUY or SELL limit order at the given price point. Limit
// orders at the exchange are canceled and recreated automatically as the
// ticker price crosses the cancel threshold and comes closer to the
// limit-price.
func New(uid string, product exchange.Product, point *point.Point) (*Limiter, error) {
	v := &Limiter{
		product:  product,
		key:      uid,
		point:    *point,
		idgen:    newIDGenerator(uid, 0),
		tickerCh: product.TickerCh(),
		orderMap: make(map[exchange.OrderID]*exchange.Order),
	}
	if err := v.check(); err != nil {
		return nil, err
	}

	return v, nil
}

func (v *Limiter) check() error {
	if len(v.key) == 0 || !path.IsAbs(v.key) {
		return fmt.Errorf("limiter uid/key %q is invalid", v.key)
	}
	if err := v.point.Check(); err != nil {
		return fmt.Errorf("limiter buy/sell point is invalid: %w", err)
	}
	return nil
}

func (v *Limiter) String() string {
	return "limiter:" + v.key
}

func (v *Limiter) Side() string {
	return v.point.Side()
}

func (v *Limiter) Status() *Status {
	return &Status{
		UID:       v.key,
		ProductID: v.product.ID(),
		Side:      v.point.Side(),
		Point:     v.point,
		Pending:   v.Pending(),
	}
}

func (v *Limiter) GetOrders(ctx context.Context) ([]*exchange.Order, error) {
	if err := v.fetchOrderMap(ctx, len(v.orderMap)); err != nil {
		return nil, err
	}
	v.compactOrderMap()

	var orders []*exchange.Order
	for _, order := range v.orderMap {
		orders = append(orders, order)
	}
	return orders, nil
}

func (v *Limiter) Pending() decimal.Decimal {
	var filled decimal.Decimal
	for _, order := range v.orderMap {
		filled = filled.Add(order.FilledSize)
	}
	return v.point.Size.Sub(filled)
}

func (v *Limiter) Run(ctx context.Context, db kv.Database) error {
	// We also need to handle resume logic here.
	if err := v.fetchOrderMap(ctx, len(v.orderMap)); err != nil {
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
	log.Printf("%s: found %d unfinished orders", v.key, nlive)
	if nlive > 1 {
		return fmt.Errorf("found %d live orders (want 0 or 1)", nlive)
	}
	if nlive != 0 {
		v.activeOrderID = live[0].OrderID
		log.Printf("%s: reusing existing order %s as the active order", v.key, v.activeOrderID)
	}

	for p := v.Pending(); !p.IsZero(); p = v.Pending() {
		select {
		case <-ctx.Done():
			if v.activeOrderID != "" {
				log.Printf("canceling active limit order %v at point %v", v.activeOrderID, v.point)
				if err := v.cancel(context.TODO()); err != nil {
					return err
				}
			}
			return context.Cause(ctx)

		case order := <-v.orderUpdatesCh:
			if err := v.updateOrderMap(order); err != nil {
				return err
			}

		case ticker := <-v.tickerCh:
			if v.Side() == "SELL" {
				if ticker.Price.LessThanOrEqual(v.point.Cancel) {
					if v.activeOrderID != "" {
						if err := v.cancel(ctx); err != nil {
							return err
						}
					}
				}
				if ticker.Price.GreaterThan(v.point.Cancel) {
					if v.activeOrderID == "" {
						if err := v.create(ctx); err != nil {
							return err
						}
					}
				}
				continue
			}

			if v.Side() == "BUY" {
				if ticker.Price.GreaterThanOrEqual(v.point.Cancel) {
					if v.activeOrderID != "" {
						if err := v.cancel(ctx); err != nil {
							return err
						}
						_ = kv.WithReadWriter(ctx, db, v.Save)
					}
				}
				if ticker.Price.LessThan(v.point.Cancel) {
					if v.activeOrderID == "" {
						if err := v.create(ctx); err != nil {
							return err
						}
						_ = kv.WithReadWriter(ctx, db, v.Save)
					}
				}
				continue
			}
		}
	}

	log.Printf("limit order %s is complete (%v pending)", v.key, v.Pending())
	if err := v.fetchOrderMap(ctx, len(v.orderMap)); err != nil {
		return err
	}
	if err := kv.WithReadWriter(ctx, db, v.Save); err != nil {
		return err
	}
	return nil
}

func (v *Limiter) create(ctx context.Context) error {
	clientOrderID := v.idgen.NextID()
	size := v.Pending()

	var err error
	var orderID exchange.OrderID
	if v.Side() == "SELL" {
		orderID, err = v.product.LimitSell(ctx, clientOrderID.String(), size, v.point.Price)
		log.Printf("limit-sell for size %s at price %s -> %v, %v", size, v.point.Price, orderID, err)
	} else {
		orderID, err = v.product.LimitBuy(ctx, clientOrderID.String(), size, v.point.Price)
		log.Printf("limit-buy for size %s at price %s -> %v, %v", size, v.point.Price, orderID, err)
	}
	if err != nil {
		return err
	}

	v.orderMap[orderID] = &exchange.Order{
		OrderID:       orderID,
		ClientOrderID: clientOrderID.String(),
		Side:          v.Side(),
	}
	v.activeOrderID = orderID
	v.orderUpdatesCh = v.product.OrderUpdatesCh(v.activeOrderID)
	return nil
}

func (v *Limiter) cancel(ctx context.Context) error {
	if err := v.product.Cancel(ctx, v.activeOrderID); err != nil {
		return err
	}
	// v.product.Retire(v.orderID)
	v.activeOrderID = ""
	v.orderUpdatesCh = nil
	return nil
}

func (v *Limiter) compactOrderMap() {
	for id, order := range v.orderMap {
		if order.Done && order.FilledSize.IsZero() {
			delete(v.orderMap, id)
			continue
		}
	}
}

func (v *Limiter) updateOrderMap(order *exchange.Order) error {
	if _, ok := v.orderMap[order.OrderID]; !ok {
		return nil
	}
	v.orderMap[order.OrderID] = order
	if order.Done && v.activeOrderID == order.OrderID {
		v.activeOrderID = ""
	}
	return nil
}

func (v *Limiter) fetchOrderMap(ctx context.Context, n int) error {
	for id, order := range v.orderMap {
		if order.Done {
			continue
		}
		order, err := v.product.Get(ctx, id)
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

func (v *Limiter) Save(ctx context.Context, rw kv.ReadWriter) error {
	v.compactOrderMap()
	gv := &State{
		ProductID: v.product.ID(),
		Offset:    v.idgen.Offset(),
		Point:     v.point,
		OrderMap:  v.orderMap,
	}
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(gv); err != nil {
		return err
	}
	return rw.Set(ctx, v.key, &buf)
}

func Load(ctx context.Context, uid string, r kv.Reader, pmap map[string]exchange.Product) (*Limiter, error) {
	gv, err := kvutil.Get[State](ctx, r, uid)
	if err != nil {
		return nil, err
	}
	product, ok := pmap[gv.ProductID]
	if !ok {
		return nil, fmt.Errorf("product %q not found", gv.ProductID)
	}
	v := &Limiter{
		product:  product,
		key:      uid,
		point:    gv.Point,
		idgen:    newIDGenerator(uid, gv.Offset),
		tickerCh: product.TickerCh(),
		orderMap: gv.OrderMap,
	}
	if v.orderMap == nil {
		v.orderMap = make(map[exchange.OrderID]*exchange.Order)
	}
	return v, nil
}
