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

	// clientServerMap holds a mapping from client-order-id to
	// exchange-order-id. We keep this metadata to verify the correctness if
	// required.
	clientServerMap map[string]exchange.OrderID

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
		productID:       productID,
		key:             uid,
		point:           *point,
		idgen:           newIDGenerator(uid, 0),
		orderMap:        make(map[exchange.OrderID]*exchange.Order),
		clientServerMap: make(map[string]exchange.OrderID),
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
						activeOrderID, orderUpdatesCh = "", nil
					}
				}
				if ticker.Price.GreaterThan(v.point.Cancel) {
					if activeOrderID == "" {
						id, ch, err := v.create(ctx, product)
						if err != nil {
							return err
						}
						dirty = true
						activeOrderID, orderUpdatesCh = id, ch
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
						activeOrderID, orderUpdatesCh = "", nil
					}
				}
				if ticker.Price.LessThan(v.point.Cancel) {
					if activeOrderID == "" {
						id, ch, err := v.create(ctx, product)
						if err != nil {
							return err
						}
						dirty = true
						activeOrderID, orderUpdatesCh = id, ch
					}
				}
				continue
			}
		}
	}

	log.Printf("%s:%s: limit %s order is complete", v.key, v.point, v.Side())
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
		log.Printf("%s:%s: create limit %s order with client-order-id %s has failed (in %s): %v", v.key, v.point, v.Side(), clientOrderID, latency, err)
		return "", nil, err
	}

	v.orderMap[orderID] = &exchange.Order{
		OrderID:       orderID,
		ClientOrderID: clientOrderID.String(),
		Side:          v.Side(),
	}
	v.clientServerMap[clientOrderID.String()] = orderID

	log.Printf("%s:%s: created a new limit %s order %s with client-order-id %s in %s", v.key, v.point, v.Side(), orderID, clientOrderID, latency)
	return orderID, product.OrderUpdatesCh(orderID), nil
}

func (v *Limiter) cancel(ctx context.Context, product exchange.Product, activeOrderID exchange.OrderID) error {
	if err := product.Cancel(ctx, activeOrderID); err != nil {
		log.Printf("%s:%s: cancel limit %s order %s has failed: %v", v.key, v.point, v.Side(), activeOrderID, err)
		return err
	}
	log.Printf("%s:%s: canceled the limit %s order %s", v.key, v.point, v.Side(), activeOrderID)
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
		UID:               v.key,
		ProductID:         v.productID,
		Offset:            v.idgen.Offset(),
		Point:             v.point,
		OrderMap:          v.orderMap,
		ClientServerIDMap: make(map[string]string),
	}
	for k, v := range v.clientServerMap {
		gv.ClientServerIDMap[k] = string(v)
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
	if len(gv.UID) > 0 && gv.UID != uid {
		return nil, fmt.Errorf("limiter uid mismatch")
	}
	v := &Limiter{
		productID:       gv.ProductID,
		key:             uid,
		point:           gv.Point,
		idgen:           newIDGenerator(uid, gv.Offset),
		orderMap:        gv.OrderMap,
		clientServerMap: make(map[string]exchange.OrderID),
	}
	if v.orderMap == nil {
		v.orderMap = make(map[exchange.OrderID]*exchange.Order)
	}
	for kk, vv := range gv.ClientServerIDMap {
		v.clientServerMap[kk] = exchange.OrderID(vv)
	}
	return v, nil
}
