// Copyright (c) 2023 BVK Chaitanya

package teaser

import (
	"bytes"
	"context"
	"encoding/gob"
	"errors"
	"fmt"
	"os"
	"path"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/bvk/tradebot/exchange"
	"github.com/bvk/tradebot/gobs"
	"github.com/bvk/tradebot/idgen"
	"github.com/bvk/tradebot/kvutil"
	"github.com/bvk/tradebot/point"
	"github.com/bvkgo/kv"
	"github.com/shopspring/decimal"
)

const DefaultKeyspace = "/teasers/"

type teaserLoop struct {
	data *gobs.TeaserLoopData

	order *exchange.SimpleOrder

	nbuys  decimal.Decimal
	bsize  decimal.Decimal
	bfees  decimal.Decimal
	bvalue decimal.Decimal

	nsells decimal.Decimal
	ssize  decimal.Decimal
	sfees  decimal.Decimal
	svalue decimal.Decimal
}

func (v *teaserLoop) refresh() {
	// TODO: Update state from the order and other info
}

func (v *teaserLoop) nextAction(minBaseSize, minQuoteSize decimal.Decimal) string {
	holdings := v.bsize.Sub(v.ssize)
	nbuys, nsells := v.nbuys.IntPart(), v.nsells.IntPart()
	pbuy, psell := !v.nbuys.IsInteger(), !v.nsells.IsInteger()

	action := "STOP"
	switch {
	case nbuys < nsells || holdings.IsNegative():
		action = "STOP"

	case nbuys > nsells:
		action = "SELL"

	case pbuy == false && psell == false:
		action = "BUY"

	case pbuy == true && psell == true:
		// When buys and sells are both partial, then we have a bug, we must stop
		// this job completely.
		action = "STOP"

	case pbuy == false && psell == true:
		// When buy is full, but sell is partial, we should complete the sell.
		action = "SELL"

	case pbuy == true && psell == false:
		// When sell is full, but buy is partial, we should complete the buy.
		action = "BUY"
	}
	return action
}

type Teaser struct {
	runtimeLock sync.Mutex

	product  string
	exchange string

	uid string

	loops []*teaserLoop

	summary atomic.Pointer[gobs.Summary]
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
func New(uid string, productID string, point *point.Point) (*Teaser, error) {
	v := &Teaser{
		productID:       productID,
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

func (v *Teaser) check() error {
	if len(v.uid) == 0 {
		return fmt.Errorf("teaser uid is empty")
	}
	if err := v.point.Check(); err != nil {
		return fmt.Errorf("teaser buy/sell point is invalid: %w", err)
	}
	return nil
}

func (v *Teaser) String() string {
	return "teaser:" + v.uid
}

func (v *Teaser) Side() string {
	return v.point.Side()
}

func (v *Teaser) Status() *Status {
	return &Status{
		UID:       v.uid,
		ProductID: v.productID,
		Side:      v.point.Side(),
		Point:     v.point,
		Pending:   v.Pending(),
	}
}

func (v *Teaser) Pending() decimal.Decimal {
	var filled decimal.Decimal
	for _, order := range v.orderMap {
		filled = filled.Add(order.FilledSize)
	}
	return v.point.Size.Sub(filled)
}

func (v *Teaser) Save(ctx context.Context, rw kv.ReadWriter) error {
	v.compactOrderMap()
	gv := &gobs.TeaserState{
		V2: &gobs.TeaserStateV2{
			ProductID:      v.productID,
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
		return fmt.Errorf("could not encode teaser state: %w", err)
	}
	key := v.uid
	if !strings.HasPrefix(key, DefaultKeyspace) {
		v := strings.TrimPrefix(v.uid, "/wallers")
		key = path.Join(DefaultKeyspace, v)
	}
	if err := rw.Set(ctx, key, &buf); err != nil {
		return fmt.Errorf("could not save teaser state: %w", err)
	}
	_ = rw.Delete(ctx, v.uid)
	return nil
}

func Load(ctx context.Context, uid string, r kv.Reader) (*Teaser, error) {
	key := path.Join(DefaultKeyspace, uid)
	gv, err := kvutil.Get[gobs.TeaserState](ctx, r, key)
	if errors.Is(err, os.ErrNotExist) {
		gv, err = kvutil.Get[gobs.TeaserState](ctx, r, uid)
	}
	if err != nil {
		return nil, fmt.Errorf("could not load teaser state: %w", err)
	}
	gv.Upgrade()
	v := &Teaser{
		uid:       uid,
		productID: gv.V2.ProductID,
		idgen:     idgen.New(uid, gv.V2.ClientIDOffset),

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
