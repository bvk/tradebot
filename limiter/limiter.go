// Copyright (c) 2023 BVK Chaitanya

package limiter

import (
	"bytes"
	"context"
	"encoding/gob"
	"fmt"
	"path"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bvk/tradebot/exchange"
	"github.com/bvk/tradebot/gobs"
	"github.com/bvk/tradebot/idgen"
	"github.com/bvk/tradebot/kvutil"
	"github.com/bvk/tradebot/point"
	"github.com/bvk/tradebot/syncmap"
	"github.com/bvk/tradebot/trader"
	"github.com/bvkgo/kv"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

const DefaultKeyspace = "/limiters/"

type Limiter struct {
	runtimeLock sync.Mutex

	productID    string
	exchangeName string

	uid string

	point point.Point

	idgen *idgen.Generator

	// orderMap holds all orders created by the limiter. It is used by Run and
	// Save methods, so it needs to be thread-safe.
	orderMap syncmap.Map[exchange.OrderID, *exchange.Order]

	optionMap map[string]string

	// holdOpt when true, pauses the buy/sell operations by this job. This flag
	// can be updated while job is running, so it needs to be an atomic.
	holdOpt atomic.Bool

	// waitForTickerSideOpt when true, makes the job wait for ticker price to be
	// on the correct side of the execution-price (i.e, above for buy and below
	// for sell) before creating the order. This option is volatile, i.e., it is
	// reset to false immediately after it is ready.
	waitForTickerSideOpt atomic.Bool

	// sizeLimitOpt when set and non-zero, contains the max limit for buy/sell
	// orders. It's value is typically less than the total size so that large
	// orders can be avoided.
	sizeLimitOpt atomic.Pointer[decimal.Decimal]
}

var _ trader.Trader = &Limiter{}

// New creates a new BUY or SELL limit order at the given price point. Limit
// orders at the exchange are canceled and recreated automatically as the
// ticker price crosses the cancel threshold and comes closer to the
// limit-price.
func New(uid, exchangeName, productID string, point *point.Point) (*Limiter, error) {
	v := &Limiter{
		productID:    productID,
		exchangeName: exchangeName,
		uid:          uid,
		point:        *point,
		idgen:        idgen.New(uid, 0),
		optionMap:    make(map[string]string),
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

func (v *Limiter) BudgetAt(feePct float64) decimal.Decimal {
	return v.point.Value().Add(v.point.FeeAt(feePct))
}

func (v *Limiter) IsBuy() bool {
	return v.point.Side() == "BUY"
}

func (v *Limiter) IsSell() bool {
	return v.point.Side() == "SELL"
}

func (v *Limiter) dupOrderMap() map[exchange.OrderID]*exchange.Order {
	dup := make(map[exchange.OrderID]*exchange.Order)
	v.orderMap.Range(func(id exchange.OrderID, order *exchange.Order) bool {
		dup[id] = order
		return true
	})
	return dup
}

func (v *Limiter) StartTime() time.Time {
	var min time.Time
	for _, order := range v.dupOrderMap() {
		if min.IsZero() {
			min = order.CreateTime.Time
		} else if order.CreateTime.Time.Before(min) {
			min = order.CreateTime.Time
		}
	}
	return min
}

func (v *Limiter) Actions() []*gobs.Action {
	var orders []*gobs.Order
	for _, order := range v.dupOrderMap() {
		if order.Done && !order.FilledSize.IsZero() {
			gorder := &gobs.Order{
				ServerOrderID: string(order.OrderID),
				ClientOrderID: order.ClientOrderID,
				CreateTime:    gobs.RemoteTime{Time: order.CreateTime.Time},
				FinishTime:    gobs.RemoteTime{Time: order.FinishTime.Time},
				Side:          order.Side,
				Status:        order.Status,
				FilledFee:     order.Fee,
				FilledSize:    order.FilledSize,
				FilledPrice:   order.FilledPrice,
				Done:          order.Done,
				DoneReason:    order.DoneReason,
			}
			orders = append(orders, gorder)
		}
	}
	sort.Slice(orders, func(i, j int) bool {
		return orders[i].CreateTime.Before(orders[j].CreateTime.Time)
	})
	if len(orders) == 0 {
		return nil
	}
	return []*gobs.Action{{UID: v.uid, Point: gobs.Point(v.point), Orders: orders}}
}

func (v *Limiter) Fees() decimal.Decimal {
	var sum decimal.Decimal
	for _, order := range v.dupOrderMap() {
		sum = sum.Add(order.Fee)
	}
	return sum
}

func (v *Limiter) BoughtValue() decimal.Decimal {
	if v.IsBuy() {
		return v.FilledValue()
	}
	return decimal.Zero
}

func (v *Limiter) SoldValue() decimal.Decimal {
	if v.IsSell() {
		return v.FilledValue()
	}
	return decimal.Zero
}

func (v *Limiter) UnsoldValue() decimal.Decimal {
	if v.IsSell() {
		return decimal.Zero
	}
	return v.BoughtValue()
}

func (v *Limiter) FilledSize() decimal.Decimal {
	var filled decimal.Decimal
	for _, order := range v.dupOrderMap() {
		filled = filled.Add(order.FilledSize)
	}
	return filled
}

func (v *Limiter) FilledValue() decimal.Decimal {
	var value decimal.Decimal
	for _, order := range v.dupOrderMap() {
		value = value.Add(order.FilledSize.Mul(order.FilledPrice))
	}
	return value
}

func (v *Limiter) PendingSize() decimal.Decimal {
	size := v.point.Size.Sub(v.FilledSize())
	if size.LessThanOrEqual(decimal.Zero) {
		return decimal.Zero
	}
	return size
}

func (v *Limiter) PendingValue() decimal.Decimal {
	return v.PendingSize().Mul(v.point.Price)
}

func (v *Limiter) compactOrderMap() {
	v.orderMap.Range(func(id exchange.OrderID, order *exchange.Order) bool {
		if order.Done && order.FilledSize.IsZero() {
			v.orderMap.Delete(id)
		}
		return true
	})
}

func (v *Limiter) updateOrderMap(order *exchange.Order) {
	if _, ok := v.orderMap.Load(order.OrderID); ok {
		v.orderMap.Store(order.OrderID, order)
	}
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
			ServerIDOrderMap: make(map[string]*gobs.Order),
			Options:          v.optionMap,
		},
	}
	for k, v := range v.dupOrderMap() {
		order := &gobs.Order{
			ServerOrderID: string(v.OrderID),
			ClientOrderID: v.ClientOrderID,
			CreateTime:    gobs.RemoteTime{Time: v.CreateTime.Time},
			FinishTime:    gobs.RemoteTime{Time: v.FinishTime.Time},
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
	key := path.Join(DefaultKeyspace, v.uid)
	if err := rw.Set(ctx, key, &buf); err != nil {
		return fmt.Errorf("could not save limiter state: %w", err)
	}
	return nil
}

func checkUID(uid string) error {
	fs := strings.Split(uid, "/")
	if len(fs) == 0 {
		return fmt.Errorf("uid cannot be empty")
	}
	if _, err := uuid.Parse(fs[0]); err != nil {
		return fmt.Errorf("uid %q doesn't start with an uuid: %w", uid, err)
	}
	return nil
}

func cleanUID(uid string) string {
	uid = strings.TrimPrefix(uid, "/wallers/")
	uid = strings.TrimPrefix(uid, "/limiters/")
	uid = strings.TrimPrefix(uid, "/loopers/")
	return uid
}

func Load(ctx context.Context, uid string, r kv.Reader) (*Limiter, error) {
	if err := checkUID(uid); err != nil {
		return nil, err
	}
	key := path.Join(DefaultKeyspace, uid)
	gv, err := kvutil.Get[gobs.LimiterState](ctx, r, key)
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
		optionMap:    make(map[string]string),

		point: point.Point{
			Size:   gv.V2.TradePoint.Size,
			Price:  gv.V2.TradePoint.Price,
			Cancel: gv.V2.TradePoint.Cancel,
		},
	}
	for kk, vv := range gv.V2.ServerIDOrderMap {
		order := &exchange.Order{
			OrderID:       exchange.OrderID(vv.ServerOrderID),
			ClientOrderID: vv.ClientOrderID,
			CreateTime:    exchange.RemoteTime{Time: vv.CreateTime.Time},
			FinishTime:    exchange.RemoteTime{Time: vv.FinishTime.Time},
			Side:          vv.Side,
			Status:        vv.Status,
			Fee:           vv.FilledFee,
			FilledSize:    vv.FilledSize,
			FilledPrice:   vv.FilledPrice,
			Done:          vv.Done,
			DoneReason:    vv.DoneReason,
		}
		v.orderMap.Store(exchange.OrderID(kk), order)
	}
	if err := v.check(); err != nil {
		return nil, err
	}
	for opt, val := range gv.V2.Options {
		if err := v.SetOption(opt, val); err != nil {
			return nil, fmt.Errorf("could not set options: %v", err)
		}
	}
	return v, nil
}
