// Copyright (c) 2023 BVK Chaitanya

package limiter

import (
	"bytes"
	"context"
	"encoding/gob"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path"
	"time"

	"github.com/bvk/tradebot/exchange"
	"github.com/bvk/tradebot/gobs"
	"github.com/bvk/tradebot/kvutil"
	"github.com/bvk/tradebot/point"
	"github.com/bvkgo/kv"
	"github.com/shopspring/decimal"
)

const DefaultKeyspace = "/limiters"

type Limiter struct {
	productID string

	key string

	point point.Point

	idgen *idGenerator

	orderMap map[exchange.OrderID]*exchange.Order
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
func New(uid string, productID string, point *point.Point) (*Limiter, error) {
	v := &Limiter{
		productID: productID,
		key:       uid,
		point:     *point,
		idgen:     newIDGenerator(uid, 0),
		orderMap:  make(map[exchange.OrderID]*exchange.Order),
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
		ProductID: v.productID,
		Side:      v.point.Side(),
		Point:     v.point,
		Pending:   v.Pending(),
	}
}

func (v *Limiter) GetOrders(ctx context.Context, product exchange.Product) ([]*exchange.Order, error) {
	if err := v.fetchOrderMap(ctx, product, len(v.orderMap)); err != nil {
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

func (v *Limiter) Run(ctx context.Context, product exchange.Product, db kv.Database) error {
	if product.ID() != v.productID {
		return os.ErrInvalid
	}
	// We also need to handle resume logic here.
	if err := v.fetchOrderMap(ctx, product, len(v.orderMap)); err != nil {
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
	log.Printf("%s:%s: found %d unfinished orders", v.key, v.point, nlive)
	if nlive > 1 {
		return fmt.Errorf("found %d live orders (want 0 or 1)", nlive)
	}

	var activeOrderID exchange.OrderID
	if nlive != 0 {
		activeOrderID = live[0].OrderID
		log.Printf("%s:%s: reusing existing order %s as the active order", v.key, v.point, activeOrderID)
	}

	var orderUpdatesCh <-chan *exchange.Order

	dirty := true
	tickerCh := product.TickerCh()

	for p := v.Pending(); !p.IsZero(); p = v.Pending() {
		select {
		case <-ctx.Done():
			if activeOrderID != "" {
				log.Printf("%s:%s: canceling active limit order %v (%v)", v.key, v.point, activeOrderID, context.Cause(ctx))
				if err := v.cancel(context.TODO(), product, activeOrderID); err != nil {
					return err
				}
			}
			return context.Cause(ctx)

		case <-time.After(time.Second):
			if dirty {
				if kv.WithReadWriter(ctx, db, v.Save) == nil {
					dirty = false
				}
			}

		case order := <-orderUpdatesCh:
			dirty = true
			if err := v.updateOrderMap(order); err != nil {
				return err
			}
			if order.Done && order.OrderID == activeOrderID {
				activeOrderID = ""
			}

		case ticker := <-tickerCh:
			if v.Side() == "SELL" {
				if ticker.Price.LessThanOrEqual(v.point.Cancel) {
					if activeOrderID != "" {
						if err := v.cancel(ctx, product, activeOrderID); err != nil {
							return err
						}
						dirty = true
						log.Printf("%s:%s: canceled sell order %q at the exchange", v.key, v.point, activeOrderID)
						activeOrderID, orderUpdatesCh = "", nil
					}
				}
				if ticker.Price.GreaterThan(v.point.Cancel) {
					if activeOrderID == "" {
						id, ch, err := v.create(ctx, product)
						if err != nil {
							log.Printf("%s:%s: could not create new sell-order: %v", v.key, v.point, err)
							return err
						}
						dirty = true
						activeOrderID, orderUpdatesCh = id, ch
						log.Printf("%s:%s: created sell order %q at the exchange", v.key, v.point, activeOrderID)
					}
				}
				continue
			}

			if v.Side() == "BUY" {
				if ticker.Price.GreaterThanOrEqual(v.point.Cancel) {
					if activeOrderID != "" {
						if err := v.cancel(ctx, product, activeOrderID); err != nil {
							return err
						}
						dirty = true
						log.Printf("%s:%s: canceled buy order %q at the exchange", v.key, v.point, activeOrderID)
						activeOrderID, orderUpdatesCh = "", nil
					}
				}
				if ticker.Price.LessThan(v.point.Cancel) {
					if activeOrderID == "" {
						id, ch, err := v.create(ctx, product)
						if err != nil {
							log.Printf("%s:%s: could not create new buy-order: %v", v.key, v.point, err)
							return err
						}
						dirty = true
						activeOrderID, orderUpdatesCh = id, ch
						log.Printf("%s:%s: created buy order %q at the exchange", v.key, v.point, activeOrderID)
					}
				}
				continue
			}
		}
	}

	log.Printf("%s:%s: limit order is complete", v.key, v.point)
	if err := v.fetchOrderMap(ctx, product, len(v.orderMap)); err != nil {
		return err
	}
	if err := kv.WithReadWriter(ctx, db, v.Save); err != nil {
		return err
	}
	return nil
}

func (v *Limiter) create(ctx context.Context, product exchange.Product) (exchange.OrderID, <-chan *exchange.Order, error) {
	clientOrderID := v.idgen.NextID()
	size := v.Pending()

	var err error
	var orderID exchange.OrderID
	if v.Side() == "SELL" {
		orderID, err = product.LimitSell(ctx, clientOrderID.String(), size, v.point.Price)
		log.Printf("limit-sell for size %s at price %s -> %v, %v", size, v.point.Price, orderID, err)
	} else {
		orderID, err = product.LimitBuy(ctx, clientOrderID.String(), size, v.point.Price)
		log.Printf("limit-buy for size %s at price %s -> %v, %v", size, v.point.Price, orderID, err)
	}
	if err != nil {
		// FIXME: We should undo the client order id generation.
		return "", nil, err
	}

	v.orderMap[orderID] = &exchange.Order{
		OrderID:       orderID,
		ClientOrderID: clientOrderID.String(),
		Side:          v.Side(),
	}
	return orderID, product.OrderUpdatesCh(orderID), nil
}

func (v *Limiter) cancel(ctx context.Context, product exchange.Product, activeOrderID exchange.OrderID) error {
	if err := product.Cancel(ctx, activeOrderID); err != nil {
		return err
	}
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
		jsdata, _ := json.Marshal(order)
		log.Printf("%v:%s:%s: %s", v.key, v.point, id, jsdata)
		v.orderMap[id] = order
		if n--; n <= 0 {
			break
		}
	}
	return nil
}

func (v *Limiter) Save(ctx context.Context, rw kv.ReadWriter) error {
	v.compactOrderMap()
	gv := &gobs.LimiterState{
		ProductID: v.productID,
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

func Load(ctx context.Context, uid string, r kv.Reader) (*Limiter, error) {
	gv, err := kvutil.Get[gobs.LimiterState](ctx, r, uid)
	if err != nil {
		return nil, err
	}
	v := &Limiter{
		productID: gv.ProductID,
		key:       uid,
		point:     gv.Point,
		idgen:     newIDGenerator(uid, gv.Offset),
		orderMap:  gv.OrderMap,
	}
	if v.orderMap == nil {
		v.orderMap = make(map[exchange.OrderID]*exchange.Order)
	}
	return v, nil
}
