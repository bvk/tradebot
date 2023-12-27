// Copyright (c) 2023 BVK Chaitanya

package waller

import (
	"bytes"
	"context"
	"encoding/gob"
	"fmt"
	"path"
	"strings"

	"github.com/bvk/tradebot/gobs"
	"github.com/bvk/tradebot/kvutil"
	"github.com/bvk/tradebot/looper"
	"github.com/bvk/tradebot/point"
	"github.com/bvk/tradebot/trader"
	"github.com/bvkgo/kv"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

const DefaultKeyspace = "/wallers/"

type Waller struct {
	uid string

	productID    string
	exchangeName string

	pairs []*point.Pair

	loopers []*looper.Looper
}

var _ trader.Trader = &Waller{}

func New(uid, exchangeName, productID string, pairs []*point.Pair) (*Waller, error) {
	w := &Waller{
		uid:          uid,
		productID:    productID,
		exchangeName: exchangeName,
		pairs:        pairs,
	}
	if err := w.check(); err != nil {
		return nil, err
	}
	var loopers []*looper.Looper
	for i, p := range pairs {
		uid := path.Join(uid, fmt.Sprintf("loop-%06d", i))
		l, err := looper.New(uid, exchangeName, productID, &p.Buy, &p.Sell)
		if err != nil {
			return nil, err
		}
		loopers = append(loopers, l)
	}
	w.loopers = loopers
	return w, nil
}

func (w *Waller) check() error {
	if len(w.uid) == 0 {
		return fmt.Errorf("waller uid is empty")
	}
	for i, p := range w.pairs {
		if err := p.Check(); err != nil {
			return fmt.Errorf("buy/sell pair %d is invalid: %w", i, err)
		}
	}
	return nil
}

func (w *Waller) String() string {
	return "waller:" + w.uid
}

func (w *Waller) UID() string {
	return w.uid
}

func (w *Waller) ProductID() string {
	return w.productID
}

func (w *Waller) ExchangeName() string {
	return w.exchangeName
}

func (w *Waller) BudgetAt(feePct float64) decimal.Decimal {
	var sum decimal.Decimal
	for _, l := range w.loopers {
		sum = sum.Add(l.BudgetAt(feePct))
	}
	return sum
}

func (w *Waller) Actions() []*gobs.Action {
	var actions []*gobs.Action
	for _, l := range w.loopers {
		if as := l.Actions(); as != nil {
			actions = append(actions, as...)
		}
	}
	return actions
}

func (w *Waller) Fees() decimal.Decimal {
	var sum decimal.Decimal
	for _, l := range w.loopers {
		sum = sum.Add(l.Fees())
	}
	return sum
}

func (w *Waller) BoughtValue() decimal.Decimal {
	var sum decimal.Decimal
	for _, l := range w.loopers {
		sum = sum.Add(l.BoughtValue())
	}
	return sum
}

func (w *Waller) SoldValue() decimal.Decimal {
	var sum decimal.Decimal
	for _, l := range w.loopers {
		sum = sum.Add(l.SoldValue())
	}
	return sum
}

func (w *Waller) UnsoldValue() decimal.Decimal {
	var sum decimal.Decimal
	for _, l := range w.loopers {
		sum = sum.Add(l.UnsoldValue())
	}
	return sum
}

func (w *Waller) Save(ctx context.Context, rw kv.ReadWriter) error {
	var loopers []string
	for _, l := range w.loopers {
		if err := l.Save(ctx, rw); err != nil {
			return fmt.Errorf("could not save looper state: %w", err)
		}
		loopers = append(loopers, l.UID())
	}
	gv := &gobs.WallerState{
		V2: &gobs.WallerStateV2{
			ProductID:    w.productID,
			ExchangeName: w.exchangeName,
			LooperIDs:    loopers,
			TradePairs:   make([]*gobs.Pair, len(w.pairs)),
		},
	}
	for i, p := range w.pairs {
		gv.V2.TradePairs[i] = &gobs.Pair{
			Buy:  gobs.Point(p.Buy),
			Sell: gobs.Point(p.Sell),
		}
	}
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(gv); err != nil {
		return fmt.Errorf("could not encode waller state: %w", err)
	}
	key := path.Join(DefaultKeyspace, w.uid)
	if err := rw.Set(ctx, key, &buf); err != nil {
		return fmt.Errorf("could not save waller state: %w", err)
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

func Load(ctx context.Context, uid string, r kv.Reader) (*Waller, error) {
	if err := checkUID(uid); err != nil {
		return nil, err
	}
	key := path.Join(DefaultKeyspace, uid)
	gv, err := kvutil.Get[gobs.WallerState](ctx, r, key)
	if err != nil {
		return nil, err
	}
	gv.Upgrade()
	var loopers []*looper.Looper
	for _, id := range gv.V2.LooperIDs {
		id := strings.TrimPrefix(id, DefaultKeyspace) // TODO: Remove after prod rollout.
		v, err := looper.Load(ctx, cleanUID(id), r)
		if err != nil {
			return nil, err
		}
		loopers = append(loopers, v)
	}
	w := &Waller{
		uid:          uid,
		productID:    gv.V2.ProductID,
		exchangeName: gv.V2.ExchangeName,
		loopers:      loopers,
		pairs:        make([]*point.Pair, len(gv.V2.TradePairs)),
	}
	for i, p := range gv.V2.TradePairs {
		w.pairs[i] = &point.Pair{
			Buy:  point.Point(p.Buy),
			Sell: point.Point(p.Sell),
		}
	}
	if err := w.check(); err != nil {
		return nil, err
	}
	return w, nil
}
