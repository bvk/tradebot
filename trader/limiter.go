// Copyright (c) 2023 BVK Chaitanya

package trader

import (
	"bytes"
	"context"
	"encoding/gob"
	"fmt"
	"log"
	"log/slog"

	"github.com/bvkgo/kv"
	"github.com/bvkgo/tradebot/exchange"
	"github.com/shopspring/decimal"
)

type Limiter struct {
	product exchange.Product

	key string

	side string // BUY or SELL

	size decimal.Decimal

	price decimal.Decimal

	cancelPrice decimal.Decimal

	idgen *idGenerator

	tickerCh <-chan *exchange.Ticker

	orderUpdatesCh <-chan *exchange.Order

	activeOrderID exchange.OrderID

	orderMap map[exchange.OrderID]*exchange.Order
}

// NewLimiter creates a new BUY or SELL limit order at the given price and
// size.
//
// Limit order will be a SELL order when cancel-price is lower than the
// limit-price or it will be a BUY order when cancel-price is higher than the
// limit-price. Limit price must never be equal to the cancel-price.
func NewLimiter(uid string, product exchange.Product, size, limitPrice, cancelPrice decimal.Decimal) (*Limiter, error) {
	side := ""
	if cancelPrice.LessThan(limitPrice) {
		side = "SELL"
	} else if cancelPrice.GreaterThan(limitPrice) {
		side = "BUY"
	} else {
		return nil, fmt.Errorf("limit price and cancel price cannot be the same")
	}

	v := &Limiter{
		product:     product,
		key:         uid,
		size:        size,
		side:        side,
		price:       limitPrice,
		cancelPrice: cancelPrice,
		idgen:       newIDGenerator(uid, 0),
		tickerCh:    product.TickerCh(),
		orderMap:    make(map[exchange.OrderID]*exchange.Order),
	}
	return v, nil
}

func (v *Limiter) UID() string {
	return v.key
}

func (v *Limiter) Side() string {
	return v.side
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
	return v.size.Sub(filled)
}

func (v *Limiter) Run(ctx context.Context, db kv.Database) error {
	for p := v.pending(); !p.IsZero(); p = v.pending() {
		select {
		case <-ctx.Done():
			if v.activeOrderID != "" {
				slog.Info("cancelling active limit order", "side", v.side, "orderID", v.activeOrderID)
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

			if v.side == "SELL" {
				if ticker.Price.LessThanOrEqual(v.cancelPrice) {
					if v.activeOrderID != "" {
						if err := v.cancel(ctx); err != nil {
							return err
						}
					}
				}
				if ticker.Price.GreaterThan(v.cancelPrice) {
					if v.activeOrderID == "" {
						if err := v.create(ctx); err != nil {
							return err
						}
					}
				}
				continue
			}

			if v.side == "BUY" {
				if ticker.Price.GreaterThanOrEqual(v.cancelPrice) {
					if v.activeOrderID != "" {
						if err := v.cancel(ctx); err != nil {
							return err
						}
						_ = kv.WithTransaction(ctx, db, v.save)
					}
				}
				if ticker.Price.LessThan(v.cancelPrice) {
					if v.activeOrderID == "" {
						if err := v.create(ctx); err != nil {
							return err
						}
						_ = kv.WithTransaction(ctx, db, v.save)
					}
				}
				continue
			}
		}
	}

	if err := v.fetchOrderMap(ctx, len(v.orderMap)); err != nil {
		return err
	}
	if err := kv.WithTransaction(ctx, db, v.save); err != nil {
		return err
	}
	return nil
}

func (v *Limiter) create(ctx context.Context) error {
	clientOrderID := v.idgen.NextID()
	size := v.pending()

	var err error
	var orderID exchange.OrderID
	if v.side == "SELL" {
		orderID, err = v.product.LimitSell(ctx, clientOrderID.String(), size, v.price)
		log.Printf("limit-sell for size %s at price %s -> %v, %v", size, v.price, orderID, err)
	} else {
		orderID, err = v.product.LimitBuy(ctx, clientOrderID.String(), size, v.price)
		log.Printf("limit-buy for size %s at price %s -> %v, %v", size, v.price, orderID, err)
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
	ProductID   string
	Offset      uint64
	Side        string
	Size        decimal.Decimal
	LimitPrice  decimal.Decimal
	CancelPrice decimal.Decimal
	OrderMap    map[exchange.OrderID]*exchange.Order
}

func (v *Limiter) save(ctx context.Context, tx kv.Transaction) error {
	v.compactOrderMap()
	gv := &gobLimiter{
		ProductID: v.product.ID(),

		Offset: v.idgen.Offset(),

		Side:        v.side,
		Size:        v.size,
		LimitPrice:  v.price,
		CancelPrice: v.cancelPrice,
		OrderMap:    v.orderMap,
	}
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(gv); err != nil {
		return err
	}
	return tx.Set(ctx, v.key, &buf)
}

func LoadLimiter(ctx context.Context, uid string, db kv.Database, pmap map[string]exchange.Product) (*Limiter, error) {
	gv, err := kvGet[gobLimiter](ctx, db, uid)
	if err != nil {
		return nil, err
	}
	product, ok := pmap[gv.ProductID]
	if !ok {
		return nil, fmt.Errorf("product %q not found", gv.ProductID)
	}
	v := &Limiter{
		product:     product,
		key:         uid,
		side:        gv.Side,
		size:        gv.Size,
		price:       gv.LimitPrice,
		cancelPrice: gv.CancelPrice,
		idgen:       newIDGenerator(uid, gv.Offset),
		tickerCh:    product.TickerCh(),
		orderMap:    gv.OrderMap,
	}
	if v.orderMap == nil {
		v.orderMap = make(map[exchange.OrderID]*exchange.Order)
	}
	return v, nil
}
