// Copyright (c) 2023 BVK Chaitanya

package limiter

import (
	"bytes"
	"context"
	"encoding/gob"
	"errors"
	"fmt"
	"log"
	"os"
	"path"
	"strings"
	"time"

	"github.com/bvk/tradebot/exchange"
	"github.com/bvk/tradebot/gobs"
	"github.com/bvk/tradebot/idgen"
	"github.com/bvk/tradebot/kvutil"
	"github.com/bvk/tradebot/point"
	"github.com/bvkgo/kv"
	"github.com/shopspring/decimal"
)

const DefaultKeyspace = "/limiters/"

type Limiter struct {
	productID    string
	exchangeName string

	uid string

	point point.Point

	idgen *idgen.Generator

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
func New(uid, exchangeName, productID string, point *point.Point) (*Limiter, error) {
	v := &Limiter{
		productID:       productID,
		exchangeName:    exchangeName,
		uid:             uid,
		point:           *point,
		idgen:           idgen.New(uid, 0),
		orderMap:        make(map[exchange.OrderID]*exchange.Order),
		clientServerMap: make(map[string]exchange.OrderID),
	}
	if err := v.check(); err != nil {
		return nil, err
	}
	return v, nil
}

func (v *Limiter) check() error {
	if len(v.uid) == 0 {
		return fmt.Errorf("limiter uid is empty")
	}
	if err := v.point.Check(); err != nil {
		return fmt.Errorf("limiter buy/sell point is invalid: %w", err)
	}
	return nil
}

func (v *Limiter) String() string {
	return "limiter:" + v.uid
}

func (v *Limiter) UID() string {
	return v.uid
}

func (v *Limiter) ProductID() string {
	return v.productID
}

func (v *Limiter) ExchangeName() string {
	return v.exchangeName
}

func (v *Limiter) Side() string {
	return v.point.Side()
}

func (v *Limiter) Status() *Status {
	return &Status{
		UID:       v.uid,
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

// Fix is a temporary helper interface used to fix any past mistakes.
func (v *Limiter) Fix(ctx context.Context, product exchange.Product, db kv.Database) error {
	v.idgen = idgen.New(v.idgen.Seed(), v.idgen.Offset()+1000000)
	return nil
}

func (v *Limiter) Run(ctx context.Context, product exchange.Product, db kv.Database) error {
	if product.ProductID() != v.productID {
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
	var orderUpdatesCh <-chan *exchange.Order
	if nlive != 0 {
		activeOrderID = live[0].OrderID
		orderUpdatesCh = product.OrderUpdatesCh(activeOrderID)
		log.Printf("%s:%s: reusing existing order %s as the active order", v.uid, v.point, activeOrderID)
	}

	dirty := 0
	tickerCh := product.TickerCh()
	flushCh := time.After(time.Minute)

	localCtx := context.Background()
	for p := v.Pending(); !p.IsZero(); p = v.Pending() {
		select {
		case <-ctx.Done():
			if activeOrderID != "" {
				log.Printf("%s:%s: canceling active limit order %v (%v)", v.uid, v.point, activeOrderID, context.Cause(ctx))
				if err := v.cancel(localCtx, product, activeOrderID); err != nil {
					return err
				}
				dirty++
			}
			if err := kv.WithReadWriter(localCtx, db, v.Save); err != nil {
				log.Printf("%s:%s dirty limit %s state could not be saved to the database (will retry): %v", v.uid, v.point, v.Side(), err)
			}
			return context.Cause(ctx)

		case <-flushCh:
			if dirty > 0 {
				if err := kv.WithReadWriter(ctx, db, v.Save); err != nil {
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
						if err := v.cancel(localCtx, product, activeOrderID); err != nil {
							return err
						}
						dirty++
						activeOrderID, orderUpdatesCh = "", nil
					}
				}
				if ticker.Price.GreaterThan(v.point.Cancel) {
					if activeOrderID == "" {
						id, ch, err := v.create(localCtx, product)
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
						if err := v.cancel(localCtx, product, activeOrderID); err != nil {
							return err
						}
						dirty++
						activeOrderID, orderUpdatesCh = "", nil
					}
				}
				if ticker.Price.LessThan(v.point.Cancel) {
					if activeOrderID == "" {
						id, ch, err := v.create(localCtx, product)
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

	if err := v.fetchOrderMap(ctx, product, len(v.orderMap)); err != nil {
		return err
	}
	if err := kv.WithReadWriter(ctx, db, v.Save); err != nil {
		return err
	}
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
		V2: &gobs.LimiterStateV2{
			ProductID:      v.productID,
			ExchangeName:   v.exchangeName,
			ClientIDSeed:   v.idgen.Seed(),
			ClientIDOffset: v.idgen.Offset(),
			TradePoint: gobs.Point{
				Size:   v.point.Size,
				Price:  v.point.Price,
				Cancel: v.point.Cancel,
			},
			ClientServerIDMap: make(map[string]string),
			ServerIDOrderMap:  make(map[string]*gobs.Order),
		},
	}
	for k, v := range v.clientServerMap {
		gv.V2.ClientServerIDMap[k] = string(v)
	}
	for k, v := range v.orderMap {
		order := &gobs.Order{
			ServerOrderID: string(v.OrderID),
			ClientOrderID: v.ClientOrderID,
			CreateTime:    gobs.RemoteTime{Time: v.CreateTime.Time},
			Side:          v.Side,
			Status:        v.Status,
			FilledFee:     v.Fee,
			FilledSize:    v.FilledSize,
			FilledPrice:   v.FilledPrice,
			Done:          v.Done,
			DoneReason:    v.DoneReason,
		}
		gv.V2.ServerIDOrderMap[string(k)] = order
	}
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(gv); err != nil {
		return fmt.Errorf("could not encode limiter state: %w", err)
	}
	key := v.uid
	if !strings.HasPrefix(key, DefaultKeyspace) {
		v := strings.TrimPrefix(v.uid, "/wallers")
		key = path.Join(DefaultKeyspace, v)
	}
	if err := rw.Set(ctx, key, &buf); err != nil {
		return fmt.Errorf("could not save limiter state: %w", err)
	}
	return nil
}

func Load(ctx context.Context, uid string, r kv.Reader) (*Limiter, error) {
	key := uid
	if !strings.HasPrefix(key, DefaultKeyspace) {
		v := strings.TrimPrefix(uid, "/wallers")
		key = path.Join(DefaultKeyspace, v)
	}
	gv, err := kvutil.Get[gobs.LimiterState](ctx, r, key)
	if errors.Is(err, os.ErrNotExist) {
		gv, err = kvutil.Get[gobs.LimiterState](ctx, r, uid)
	}
	if err != nil {
		return nil, fmt.Errorf("could not load limiter state: %w", err)
	}
	gv.Upgrade()
	seed := uid
	if len(gv.V2.ClientIDSeed) > 0 {
		seed = gv.V2.ClientIDSeed
	}
	v := &Limiter{
		uid:          uid,
		productID:    gv.V2.ProductID,
		exchangeName: gv.V2.ExchangeName,
		idgen:        idgen.New(seed, gv.V2.ClientIDOffset),

		point: point.Point{
			Size:   gv.V2.TradePoint.Size,
			Price:  gv.V2.TradePoint.Price,
			Cancel: gv.V2.TradePoint.Cancel,
		},

		orderMap:        make(map[exchange.OrderID]*exchange.Order),
		clientServerMap: make(map[string]exchange.OrderID),
	}
	for kk, vv := range gv.V2.ClientServerIDMap {
		v.clientServerMap[kk] = exchange.OrderID(vv)
	}
	for kk, vv := range gv.V2.ServerIDOrderMap {
		order := &exchange.Order{
			OrderID:       exchange.OrderID(vv.ServerOrderID),
			ClientOrderID: vv.ClientOrderID,
			CreateTime:    exchange.RemoteTime{Time: vv.CreateTime.Time},
			Side:          vv.Side,
			Status:        vv.Status,
			Fee:           vv.FilledFee,
			FilledSize:    vv.FilledSize,
			FilledPrice:   vv.FilledPrice,
			Done:          vv.Done,
			DoneReason:    vv.DoneReason,
		}
		v.orderMap[exchange.OrderID(kk)] = order
	}
	return v, nil
}
