// Copyright (c) 2023 BVK Chaitanya

package limiter

import (
	"bytes"
	"context"
	"encoding/gob"
	"fmt"
	"log"
	"log/slog"
	"path"

	"github.com/bvkgo/kv"
	"github.com/bvkgo/tradebot/exchange"
	"github.com/bvkgo/tradebot/kvutil"
	"github.com/bvkgo/tradebot/point"
	"github.com/shopspring/decimal"
)

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

// New creates a new BUY or SELL limit order at the given price and size.
//
// Limit order will be a SELL order when cancel-price is lower than the
// limit-price or it will be a BUY order when cancel-price is higher than the
// limit-price. Limit price must never be equal to the cancel-price.
func New(uid string, product exchange.Product, point *point.Point) (*Limiter, error) {
	if err := point.Check(); err != nil {
		return nil, err
	}

	v := &Limiter{
		product:  product,
		key:      uid,
		point:    *point,
		idgen:    newIDGenerator(uid, 0),
		tickerCh: product.TickerCh(),
		orderMap: make(map[exchange.OrderID]*exchange.Order),
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

func (v *Limiter) UID() string {
	return v.key
}

func (v *Limiter) Side() string {
	return v.point.Side()
}

func (v *Limiter) GetOrders(ctx context.Context) ([]*exchange.Order, error) {
	if err := v.fetchOrderMap(ctx, len(v.orderMap)); err != nil {
		return nil, err
	}
	v.compactOrderMap()

	var orders []*exchange.Order
	for _, order := range v.orderMap {
		if order != nil {
			orders = append(orders, order)
		}
	}
	return orders, nil
}

func (v *Limiter) pending() decimal.Decimal {
	var filled decimal.Decimal
	for _, order := range v.orderMap {
		if order != nil {
			filled = filled.Add(order.FilledSize)
		}
	}
	return v.point.Size.Sub(filled)
}

func (v *Limiter) Run(ctx context.Context, db kv.Database) error {
	for p := v.pending(); !p.IsZero(); p = v.pending() {
		select {
		case <-ctx.Done():
			if v.activeOrderID != "" {
				slog.Info("cancelling active limit order", "point", v.point, "orderID", v.activeOrderID)
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
			// slog.InfoContext(ctx, "change", "ticker", ticker.Price, "orderID", v.orderID, "updatesCh", v.orderUpdatesCh != nil)

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
						_ = kv.WithTransaction(ctx, db, v.Save)
					}
				}
				if ticker.Price.LessThan(v.point.Cancel) {
					if v.activeOrderID == "" {
						if err := v.create(ctx); err != nil {
							return err
						}
						_ = kv.WithTransaction(ctx, db, v.Save)
					}
				}
				continue
			}
		}
	}

	if err := v.fetchOrderMap(ctx, len(v.orderMap)); err != nil {
		return err
	}
	if err := kv.WithTransaction(ctx, db, v.Save); err != nil {
		return err
	}
	return nil
}

func (v *Limiter) create(ctx context.Context) error {
	clientOrderID := v.idgen.NextID()
	size := v.pending()

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

	v.orderMap[orderID] = nil
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
		if order != nil && order.Done && order.FilledSize.IsZero() {
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

func (v *Limiter) fetchOrderMap(ctx context.Context, n int) error {
	for id, order := range v.orderMap {
		if order != nil && order.Done {
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

type gobLimiter struct {
	ProductID string
	Offset    uint64
	Point     point.Point
	OrderMap  map[exchange.OrderID]*exchange.Order
}

func (v *Limiter) Save(ctx context.Context, tx kv.Transaction) error {
	v.compactOrderMap()
	gv := &gobLimiter{
		ProductID: v.product.ID(),
		Offset:    v.idgen.Offset(),
		Point:     v.point,
		OrderMap:  v.orderMap,
	}
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(gv); err != nil {
		return err
	}
	return tx.Set(ctx, v.key, &buf)
}

func Load(ctx context.Context, uid string, r kv.Reader, pmap map[string]exchange.Product) (*Limiter, error) {
	gv, err := kvutil.Get[gobLimiter](ctx, r, uid)
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
