// Copyright (c) 2023 BVK Chaitanya

package waller

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
	"sync"
	"time"

	"github.com/bvk/tradebot/exchange"
	"github.com/bvk/tradebot/gobs"
	"github.com/bvk/tradebot/kvutil"
	"github.com/bvk/tradebot/looper"
	"github.com/bvk/tradebot/point"
	"github.com/bvkgo/kv"
)

const DefaultKeyspace = "/wallers/"

type Waller struct {
	uid string

	productID    string
	exchangeName string

	pairs []*point.Pair

	loopers []*looper.Looper
}

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

func (w *Waller) Run(ctx context.Context, product exchange.Product, db kv.Database) error {
	var wg sync.WaitGroup

	for _, loop := range w.loopers {
		loop := loop

		wg.Add(1)
		go func() {
			defer wg.Done()

			for ctx.Err() == nil {
				if err := loop.Run(ctx, product, db); err != nil {
					if ctx.Err() == nil {
						log.Printf("wall-looper %v has failed (retry): %v", loop, err)
						time.Sleep(time.Second)
					}
				}
			}
		}()
	}

	wg.Wait()
	return context.Cause(ctx)
}

func (w *Waller) Save(ctx context.Context, rw kv.ReadWriter) error {
	var loopers []string
	for _, l := range w.loopers {
		if err := l.Save(ctx, rw); err != nil {
			return fmt.Errorf("could not save looper state: %w", err)
		}
		s := l.Status()
		loopers = append(loopers, s.UID)
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
	key := w.uid
	if !strings.HasPrefix(key, DefaultKeyspace) {
		key = path.Join(DefaultKeyspace, w.uid)
	}
	if err := rw.Set(ctx, key, &buf); err != nil {
		return fmt.Errorf("could not save waller state: %w", err)
	}
	return nil
}

func Load(ctx context.Context, uid string, r kv.Reader) (*Waller, error) {
	key := uid
	if !strings.HasPrefix(key, DefaultKeyspace) {
		key = path.Join(DefaultKeyspace, uid)
	}
	gv, err := kvutil.Get[gobs.WallerState](ctx, r, key)
	if errors.Is(err, os.ErrNotExist) {
		gv, err = kvutil.Get[gobs.WallerState](ctx, r, uid)
	}
	if err != nil {
		return nil, err
	}
	gv.Upgrade()
	var loopers []*looper.Looper
	for _, id := range gv.V2.LooperIDs {
		id := strings.TrimPrefix(id, DefaultKeyspace) // TODO: Remove after prod rollout.
		v, err := looper.Load(ctx, id, r)
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
