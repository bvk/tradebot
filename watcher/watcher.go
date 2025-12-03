// Copyright (c) 2025 BVK Chaitanya

package watcher

import (
	"bytes"
	"context"
	"encoding/gob"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/bvk/tradebot/exchange"
	"github.com/bvk/tradebot/gobs"
	"github.com/bvk/tradebot/kvutil"
	"github.com/bvk/tradebot/point"
	"github.com/bvk/tradebot/timerange"
	"github.com/bvk/tradebot/trader"
	"github.com/bvkgo/kv"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/visvasity/topic"
)

const DefaultKeyspace = "/watchers/"

type Watcher struct {
	runtimeLock sync.Mutex

	uid string

	state *gobs.WatcherState
}

var _ trader.Trader = &Watcher{}

func New(uid, exchange, product string, feePct decimal.Decimal, pairs []*point.Pair) (*Watcher, error) {
	var loops []*gobs.WatcherLoop
	for _, p := range pairs {
		loop := &gobs.WatcherLoop{
			Pair: gobs.Pair{
				Buy:  gobs.Point(p.Buy),
				Sell: gobs.Point(p.Sell),
			},
		}
		loops = append(loops, loop)
	}

	w := &Watcher{
		uid: uid,
		state: &gobs.WatcherState{
			ExchangeName: exchange,
			ProductID:    product,
			FeePct:       feePct,
			TradeLoops:   loops,
		},
	}

	if err := w.check(); err != nil {
		return nil, err
	}
	return w, nil
}

func Load(ctx context.Context, uid string, r kv.Reader) (*Watcher, error) {
	fs := strings.Split(uid, "/")
	if len(fs) == 0 {
		return nil, fmt.Errorf("uid cannot be empty")
	}
	if _, err := uuid.Parse(fs[0]); err != nil {
		return nil, fmt.Errorf("uid %q doesn't start with an uuid: %w", uid, err)
	}
	key := path.Join(DefaultKeyspace, uid)
	state, err := kvutil.Get[gobs.WatcherState](ctx, r, key)
	if err != nil {
		return nil, err
	}
	w := &Watcher{
		uid:   uid,
		state: state,
	}
	if err := w.check(); err != nil {
		return nil, err
	}
	return w, nil
}

func (w *Watcher) Save(ctx context.Context, rw kv.ReadWriter) error {
	w.state.LifetimeSummary = w.GetSummary(nil)
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(&w.state); err != nil {
		return fmt.Errorf("could not encode watcher state: %w", err)
	}
	key := path.Join(DefaultKeyspace, w.uid)
	if err := rw.Set(ctx, key, &buf); err != nil {
		return fmt.Errorf("could not save watcher state: %w", err)
	}
	return nil
}

func (w *Watcher) check() error {
	if len(w.uid) == 0 {
		return fmt.Errorf("watcher uid is empty")
	}
	if len(w.state.TradeLoops) == 0 {
		return fmt.Errorf("number of pairs cannot be zero")
	}
	fs := strings.Split(w.uid, "/")
	if len(fs) == 0 {
		return fmt.Errorf("uid cannot be empty")
	}
	if _, err := uuid.Parse(fs[0]); err != nil {
		return fmt.Errorf("uid %q doesn't start with an uuid: %w", w.uid, err)
	}
	for i, p := range w.state.TradeLoops {
		x := point.Pair{
			Buy:  point.Point(p.Pair.Buy),
			Sell: point.Point(p.Pair.Sell),
		}
		if err := x.Check(); err != nil {
			return fmt.Errorf("buy/sell pair %d is invalid: %w", i, err)
		}
	}
	return nil
}

func (w *Watcher) String() string {
	return "watcher:" + w.uid
}

func (w *Watcher) UID() string {
	return w.uid
}

func (w *Watcher) ProductID() string {
	return w.state.ProductID
}

func (w *Watcher) ExchangeName() string {
	return w.state.ExchangeName
}

func (w *Watcher) SetOption(key, value string) error {
	return fmt.Errorf("watcher job doesn't support option %q", key)
}

func (w *Watcher) Actions() []*gobs.Action {
	return []*gobs.Action{}
}

func (w *Watcher) BudgetAt(feePct decimal.Decimal) decimal.Decimal {
	var sum decimal.Decimal
	for _, p := range w.state.TradeLoops {
		b := point.Point(p.Pair.Buy)
		sum = sum.Add(b.Value().Add(b.FeeAt(feePct)))
	}
	return sum
}

var d100 = decimal.NewFromInt(100)

func (w *Watcher) GetSummary(r *timerange.Range) *gobs.Summary {
	s := &gobs.Summary{
		Exchange:  w.state.ExchangeName,
		ProductID: w.state.ProductID,
		Budget:    w.BudgetAt(decimal.Zero),
	}

	for _, loop := range w.state.TradeLoops {
		sells, buys := loop.Sells, loop.Buys
		if r != nil {
			sells, buys = []time.Time{}, []time.Time{}
			for i, stime := range loop.Sells {
				if r.InRange(stime) {
					sells = append(sells, loop.Sells[i])
					buys = append(buys, loop.Buys[i])
				}
			}
		}

		var smallest, largest time.Time
		if len(buys)+len(sells) != 0 {
			smallest = slices.MinFunc(append(buys, sells...), func(a, b time.Time) int { return a.Compare(b) })
			largest = slices.MaxFunc(append(buys, sells...), func(a, b time.Time) int { return a.Compare(b) })
		}
		if s.BeginAt.IsZero() || smallest.Before(s.BeginAt) {
			s.BeginAt = smallest
		}
		if s.EndAt.IsZero() || largest.After(s.EndAt) {
			s.EndAt = largest
		}

		numBuys := decimal.NewFromInt(int64(len(buys)))
		numSells := decimal.NewFromInt(int64(len(sells)))
		bpoint, spoint := point.Point(loop.Pair.Buy), point.Point(loop.Pair.Sell)

		s.BoughtSize = s.BoughtSize.Add(bpoint.Size.Mul(numBuys))
		s.BoughtValue = s.BoughtValue.Add(bpoint.Value().Mul(numBuys))

		s.SoldSize = s.SoldSize.Add(spoint.Size.Mul(numSells))
		s.SoldValue = s.SoldValue.Add(spoint.Value().Mul(numSells))

		if n := len(buys) - len(sells); n > 0 {
			usize := decimal.NewFromInt(int64(n))
			s.UnsoldSize = s.UnsoldSize.Add(usize)
			s.UnsoldValue = s.UnsoldValue.Add(usize.Mul(loop.Pair.Buy.Price))
		}

		s.NumBuys = s.NumBuys.Add(numBuys)
		s.NumSells = s.NumSells.Add(numSells)
	}
	s.SoldFees = s.SoldValue.Mul(w.state.FeePct).Div(d100)
	s.BoughtFees = s.BoughtValue.Mul(w.state.FeePct).Div(d100)
	s.UnsoldFees = s.UnsoldValue.Mul(w.state.FeePct).Div(d100)

	if r != nil && !r.InRange(s.EndAt) {
		return &gobs.Summary{
			Exchange:  s.Exchange,
			ProductID: s.ProductID,
			Budget:    s.Budget,
		}
	}
	return s
}

func (w *Watcher) Run(ctx context.Context, rt *trader.Runtime) error {
	w.runtimeLock.Lock()
	defer w.runtimeLock.Unlock()

	priceUpdates, err := rt.Product.GetPriceUpdates()
	if err != nil {
		return err
	}
	defer priceUpdates.Close()

	tickerCh, err := topic.ReceiveCh(priceUpdates)
	if err != nil {
		return err
	}

	var last exchange.PriceUpdate

	for ctx.Err() == nil {
		select {
		case <-ctx.Done():
			continue

		case ticker := <-tickerCh:
			nevents := 0
			for i := range w.state.TradeLoops {
				if w.handlePriceUpdate(ctx, i, last, ticker) {
					nevents++
				}
			}
			last = ticker
			if nevents != 0 {
				if err := kv.WithReadWriter(ctx, rt.Database, w.Save); err != nil {
					slog.Error("could not save job state (will retry)", "watcher", w, "err", err)
				}
			}
		}
	}
	return context.Cause(ctx)
}

func (w *Watcher) handlePriceUpdate(_ context.Context, index int, last, current exchange.PriceUpdate) bool {
	if last == nil || current == nil {
		return false
	}

	loop := w.state.TradeLoops[index]
	buy, sell := loop.Pair.Buy, loop.Pair.Sell

	lastPrice, _ := last.PricePoint()
	price, tickerTime := current.PricePoint()

	nbuys := len(loop.Buys)
	nsells := len(loop.Sells)

	action := "STOP"
	switch {
	case nbuys < nsells:
		action = "STOP"

	case nbuys == nsells:
		action = "BUY"

	case nbuys > nsells:
		action = "SELL"
	}

	switch action {
	case "BUY":
		if lastPrice.GreaterThanOrEqual(buy.Price) && price.LessThan(buy.Price) {
			loop.Buys = append(loop.Buys, tickerTime.Time)
			slog.Info("watcher: a new buy is completed", "watcher", w, "buy-price", buy.Price, "timestamp", tickerTime.Time, "buy-size", buy.Size)
			return true
		}
		return false

	case "SELL":
		if price.LessThanOrEqual(sell.Price) {
			return false
		}
		loop.Sells = append(loop.Sells, tickerTime.Time)
		slog.Info("watcher: a new sell is completed", "watcher", w, "sell-price", sell.Price, "timestamp", tickerTime.Time, "sell-size", sell.Size)
		return true

	case "STOP":
		slog.Error("could not determine next action for the watcher (ignored)", "watcher", w)
		return false

	default:
		slog.Error("invalid next action for the watcher (ignored)", "watcher", w)
		return false
	}
}

func LoadAll(ctx context.Context, r kv.Reader) ([]*Watcher, error) {
	return LoadFunc(ctx, r, nil)
}

func LoadFunc(ctx context.Context, r kv.Reader, pickf func(string) bool) ([]*Watcher, error) {
	const MinUUID = "00000000-0000-0000-0000-000000000000"
	const MaxUUID = "ffffffff-ffff-ffff-ffff-ffffffffffff"

	begin := path.Join(DefaultKeyspace, MinUUID)
	end := path.Join(DefaultKeyspace, MaxUUID)

	it, err := r.Ascend(ctx, begin, end)
	if err != nil {
		return nil, err
	}
	defer kv.Close(it)

	var watchers []*Watcher
	for k, _, err := it.Fetch(ctx, false); err == nil; k, _, err = it.Fetch(ctx, true) {
		if pickf != nil {
			if !pickf(k) {
				continue
			}
		}

		uid := strings.TrimPrefix(k, DefaultKeyspace)
		v, err := Load(ctx, uid, r)
		if err != nil {
			return nil, err
		}
		watchers = append(watchers, v)
	}

	if _, _, err := it.Fetch(ctx, false); err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	return watchers, nil
}

func Summary(ctx context.Context, r kv.Reader, uid string, period *timerange.Range) (*gobs.Summary, error) {
	v, err := Load(ctx, uid, r)
	if err != nil {
		return nil, err
	}
	return v.GetSummary(period), nil
}
