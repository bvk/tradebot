// Copyright (c) 2023 BVK Chaitanya

package trader

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
)

type Waller struct {
	key string

	product exchange.Product

	buyPoints  []*Point
	sellPoints []*Point

	loopers []*Looper
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

type gobWaller struct {
	ProductID  string
	BuyPoints  []*Point
	SellPoints []*Point
	Loopers    []string
}

func (w *Waller) save(ctx context.Context, tx kv.Transaction) error {
	var loopers []string
	for _, l := range w.loopers {
		if err := l.save(ctx, tx); err != nil {
			return err
		}
		loopers = append(loopers, l.UID())
	}
	gv := &gobWaller{
		ProductID:  w.product.ID(),
		BuyPoints:  w.buyPoints,
		SellPoints: w.sellPoints,
		Loopers:    loopers,
	}
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(gv); err != nil {
		return err
	}
	return tx.Set(ctx, w.key, &buf)
}

func LoadWaller(ctx context.Context, uid string, db kv.Database, pmap map[string]exchange.Product) (*Waller, error) {
	gv, err := kvGet[gobWaller](ctx, db, uid)
	if err != nil {
		return nil, err
	}
	product, ok := pmap[gv.ProductID]
	if !ok {
		return nil, fmt.Errorf("product %q not found", gv.ProductID)
	}
	var loopers []*Looper
	for _, id := range gv.Loopers {
		v, err := LoadLooper(ctx, id, db, pmap)
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
