// Copyright (c) 2023 BVK Chaitanya

package looper

import (
	"bytes"
	"context"
	"encoding/gob"
	"errors"
	"fmt"
	"os"
	"path"
	"sort"
	"strings"

	"github.com/bvk/tradebot/gobs"
	"github.com/bvk/tradebot/kvutil"
	"github.com/bvk/tradebot/limiter"
	"github.com/bvk/tradebot/point"
	"github.com/bvk/tradebot/trader"
	"github.com/bvkgo/kv"
	"github.com/shopspring/decimal"
)

const DefaultKeyspace = "/loopers/"

type Looper struct {
	productID    string
	exchangeName string

	uid string

	buyPoint  point.Point
	sellPoint point.Point

	buys  []*limiter.Limiter
	sells []*limiter.Limiter
}

var _ trader.Job = &Looper{}

func New(uid, exchangeName, productID string, buy, sell *point.Point) (*Looper, error) {
	v := &Looper{
		productID: productID,
		uid:       uid,
		buyPoint:  *buy,
		sellPoint: *sell,
	}
	if err := v.check(); err != nil {
		return nil, err
	}
	return v, nil
}

func (v *Looper) check() error {
	if len(v.uid) == 0 {
		return fmt.Errorf("looper uid is empty")
	}
	if err := v.buyPoint.Check(); err != nil {
		return fmt.Errorf("buy point %v is invalid", v.buyPoint)
	}
	if side := v.buyPoint.Side(); side != "BUY" {
		return fmt.Errorf("buy point %v has invalid side", v.buyPoint)
	}
	if err := v.sellPoint.Check(); err != nil {
		return fmt.Errorf("sell point %v is invalid", v.sellPoint)
	}
	if side := v.sellPoint.Side(); side != "SELL" {
		return fmt.Errorf("sell point %v has invalid side", v.sellPoint)
	}
	if v.sellPoint.Size.GreaterThan(v.buyPoint.Size) {
		return fmt.Errorf("sell size %s is more than buy size %s", v.sellPoint.Size, v.buyPoint.Size)
	}
	return nil
}

func (v *Looper) String() string {
	return "looper:" + v.uid
}

func (v *Looper) UID() string {
	return v.uid
}

func (v *Looper) ProductID() string {
	return v.productID
}

func (v *Looper) ExchangeName() string {
	return v.exchangeName
}

func (v *Looper) Actions() []*gobs.Action {
	var actions []*gobs.Action
	for _, b := range v.buys {
		if as := b.Actions(); as != nil {
			actions = append(actions, as[0])
		}
	}
	for _, s := range v.sells {
		if as := s.Actions(); as != nil {
			actions = append(actions, as[0])
		}
	}
	sort.Slice(actions, func(i, j int) bool {
		return actions[i].Orders[0].CreateTime.Time.Before(actions[j].Orders[0].CreateTime.Time)
	})
	if len(actions) == 0 {
		return nil
	}
	return actions
}

func (v *Looper) Pair() *point.Pair {
	return &point.Pair{Buy: v.buyPoint, Sell: v.sellPoint}
}

func (v *Looper) Fees() decimal.Decimal {
	var sum decimal.Decimal
	for _, b := range v.buys {
		sum = sum.Add(b.Fees())
	}
	for _, s := range v.sells {
		sum = sum.Add(s.Fees())
	}
	return sum
}

func (v *Looper) BoughtValue() decimal.Decimal {
	var sum decimal.Decimal
	for _, b := range v.buys {
		sum = sum.Add(b.FilledValue())
	}
	return sum
}

func (v *Looper) SoldValue() decimal.Decimal {
	var sum decimal.Decimal
	for _, s := range v.sells {
		sum = sum.Add(s.FilledValue())
	}
	return sum
}

func (v *Looper) UnsoldValue() decimal.Decimal {
	bsize := v.BoughtValue().Div(v.buyPoint.Price)
	ssize := v.SoldValue().Div(v.sellPoint.Price)
	if d := bsize.Sub(ssize); d.GreaterThan(decimal.Zero) {
		return d.Mul(v.buyPoint.Price)
	}
	return decimal.Zero
}

func (v *Looper) Save(ctx context.Context, rw kv.ReadWriter) error {
	var limiters []string
	for _, b := range v.buys {
		if err := b.Save(ctx, rw); err != nil {
			return fmt.Errorf("could not save child limiter: %w", err)
		}
		limiters = append(limiters, b.UID())
	}
	for _, s := range v.sells {
		if err := s.Save(ctx, rw); err != nil {
			return fmt.Errorf("could not save child limiter: %w", err)
		}
		limiters = append(limiters, s.UID())
	}
	gv := &gobs.LooperState{
		V2: &gobs.LooperStateV2{
			ProductID:    v.productID,
			ExchangeName: v.exchangeName,
			LimiterIDs:   limiters,
			TradePair: gobs.Pair{
				Buy: gobs.Point{
					Size:   v.buyPoint.Size,
					Price:  v.buyPoint.Price,
					Cancel: v.buyPoint.Cancel,
				},
				Sell: gobs.Point{
					Size:   v.sellPoint.Size,
					Price:  v.sellPoint.Price,
					Cancel: v.sellPoint.Cancel,
				},
			},
		},
	}
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(gv); err != nil {
		return fmt.Errorf("could not encode looper state: %w", err)
	}
	key := v.uid
	if !strings.HasPrefix(key, DefaultKeyspace) {
		v := strings.TrimPrefix(v.uid, "/wallers")
		key = path.Join(DefaultKeyspace, v)
	}
	if err := rw.Set(ctx, key, &buf); err != nil {
		return fmt.Errorf("could not save looper state: %w", err)
	}
	return nil
}

func Load(ctx context.Context, uid string, r kv.Reader) (*Looper, error) {
	key := uid
	if !strings.HasPrefix(key, DefaultKeyspace) {
		v := strings.TrimPrefix(uid, "/wallers")
		key = path.Join(DefaultKeyspace, v) // TODO: Make this default
	}
	gv, err := kvutil.Get[gobs.LooperState](ctx, r, key)
	if errors.Is(err, os.ErrNotExist) {
		gv, err = kvutil.Get[gobs.LooperState](ctx, r, uid) // TODO: Remove after prod rollout
	}
	if err != nil {
		return nil, err
	}
	gv.Upgrade()
	var buys, sells []*limiter.Limiter
	for _, id := range gv.V2.LimiterIDs {
		v, err := limiter.Load(ctx, id, r)
		if err != nil {
			return nil, err
		}
		if v.IsBuy() {
			buys = append(buys, v)
			continue
		}
		sells = append(sells, v)
	}

	v := &Looper{
		uid:          uid,
		productID:    gv.V2.ProductID,
		exchangeName: gv.V2.ExchangeName,
		buys:         buys,
		sells:        sells,
		buyPoint: point.Point{
			Size:   gv.V2.TradePair.Buy.Size,
			Price:  gv.V2.TradePair.Buy.Price,
			Cancel: gv.V2.TradePair.Buy.Cancel,
		},
		sellPoint: point.Point{
			Size:   gv.V2.TradePair.Sell.Size,
			Price:  gv.V2.TradePair.Sell.Price,
			Cancel: gv.V2.TradePair.Sell.Cancel,
		},
	}
	if err := v.check(); err != nil {
		return nil, err
	}
	return v, nil
}
