// Copyright (c) 2023 BVK Chaitanya

package waller

import (
	"bytes"
	"context"
	"encoding/gob"
	"fmt"
	"log/slog"
	"path"
	"sync"

	"github.com/bvkgo/kv"
	"github.com/bvkgo/tradebot/exchange"
	"github.com/bvkgo/tradebot/kvutil"
	"github.com/bvkgo/tradebot/looper"
	"github.com/bvkgo/tradebot/point"
)

type Waller struct {
	key string

	product exchange.Product

	buyPoints  []*point.Point
	sellPoints []*point.Point

	loopers []*looper.Looper
}

type State struct {
	ProductID  string
	BuyPoints  []*point.Point
	SellPoints []*point.Point
	Loopers    []string
}

type Status struct {
	UID string

	ProductID string

	BuyPoints  []*point.Point
	SellPoints []*point.Point

	// TODO: Add more status data.
}

func New(uid string, product exchange.Product, buys, sells []*point.Point) (*Waller, error) {
	w := &Waller{
		key:        uid,
		product:    product,
		buyPoints:  buys,
		sellPoints: sells,
	}
	if err := w.check(); err != nil {
		return nil, err
	}
	return w, nil
}

func (w *Waller) check() error {
	if len(w.key) == 0 || !path.IsAbs(w.key) {
		return fmt.Errorf("waller uid/key %q is invalid", w.key)
	}
	for i, b := range w.buyPoints {
		if err := b.Check(); err != nil {
			return fmt.Errorf("buy-point %d is invalid: %w", i, err)
		}
		if b.Side() != "BUY" {
			return fmt.Errorf("buy-point %d (%v) has invalid side", i, b)
		}
	}
	for i, s := range w.sellPoints {
		if err := s.Check(); err != nil {
			return fmt.Errorf("sell-point %d is invalid: %w", i, err)
		}
		if s.Side() != "SELL" {
			return fmt.Errorf("sell-point %d (%v) has invalid side", i, s)
		}
	}
	return nil
}

func (w *Waller) Status() *Status {
	return &Status{
		UID:        w.key,
		ProductID:  w.product.ID(),
		BuyPoints:  w.buyPoints,
		SellPoints: w.sellPoints,
	}
}

func (w *Waller) Run(ctx context.Context, db kv.Database) error {
	var wg sync.WaitGroup
	defer wg.Wait()

	for _, loop := range w.loopers {
		loop := loop

		wg.Add(1)
		go func() {
			defer wg.Done()

			if err := loop.Run(ctx, db); err != nil {
				slog.ErrorContext(ctx, "sub looper for wall has failed", "error", err)
				return
			}
		}()
	}
	return nil
}

func (w *Waller) Save(ctx context.Context, rw kv.ReadWriter) error {
	var loopers []string
	for _, l := range w.loopers {
		if err := l.Save(ctx, rw); err != nil {
			return err
		}
		s := l.Status()
		loopers = append(loopers, s.UID)
	}
	gv := &State{
		ProductID:  w.product.ID(),
		BuyPoints:  w.buyPoints,
		SellPoints: w.sellPoints,
		Loopers:    loopers,
	}
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(gv); err != nil {
		return err
	}
	return rw.Set(ctx, w.key, &buf)
}

func Load(ctx context.Context, uid string, r kv.Reader, pmap map[string]exchange.Product) (*Waller, error) {
	gv, err := kvutil.Get[State](ctx, r, uid)
	if err != nil {
		return nil, err
	}
	product, ok := pmap[gv.ProductID]
	if !ok {
		return nil, fmt.Errorf("product %q not found", gv.ProductID)
	}
	var loopers []*looper.Looper
	for _, id := range gv.Loopers {
		v, err := looper.Load(ctx, id, r, pmap)
		if err != nil {
			return nil, err
		}
		loopers = append(loopers, v)
	}
	w := &Waller{
		key:        uid,
		product:    product,
		loopers:    loopers,
		buyPoints:  gv.BuyPoints,
		sellPoints: gv.SellPoints,
	}
	if err := w.check(); err != nil {
		return nil, err
	}
	return w, nil
}
