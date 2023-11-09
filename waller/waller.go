// Copyright (c) 2023 BVK Chaitanya

package waller

import (
	"bytes"
	"context"
	"encoding/gob"
	"fmt"
	"log"
	"path"
	"sync"
	"time"

	"github.com/bvk/tradebot/exchange"
	"github.com/bvk/tradebot/gobs"
	"github.com/bvk/tradebot/kvutil"
	"github.com/bvk/tradebot/looper"
	"github.com/bvk/tradebot/point"
	"github.com/bvkgo/kv"
)

const DefaultKeyspace = "/wallers"

type Waller struct {
	key string

	productID string

	buyPoints  []*point.Point
	sellPoints []*point.Point

	loopers []*looper.Looper
}

type State = gobs.WallerState

type Status struct {
	UID string

	ProductID string

	BuyPoints  []*point.Point
	SellPoints []*point.Point

	// TODO: Add more status data.
}

func New(uid string, productID string, buys, sells []*point.Point) (*Waller, error) {
	w := &Waller{
		key:        uid,
		productID:  productID,
		buyPoints:  buys,
		sellPoints: sells,
	}
	if err := w.check(); err != nil {
		return nil, err
	}
	var loopers []*looper.Looper
	for i := 0; i < len(buys); i++ {
		luid := path.Join(uid, fmt.Sprintf("loop-%06d", i))
		l, err := looper.New(luid, productID, buys[i], sells[i])
		if err != nil {
			return nil, err
		}
		loopers = append(loopers, l)
	}
	w.loopers = loopers
	return w, nil
}

func (w *Waller) check() error {
	if len(w.key) == 0 || !path.IsAbs(w.key) {
		return fmt.Errorf("waller uid/key %q is invalid", w.key)
	}
	if a, b := len(w.buyPoints), len(w.sellPoints); a != b {
		return fmt.Errorf("number of buys %d must match sells %d", a, b)
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

func (w *Waller) String() string {
	return "waller:" + w.key
}

func (w *Waller) Status() *Status {
	return &Status{
		UID:        w.key,
		ProductID:  w.productID,
		BuyPoints:  w.buyPoints,
		SellPoints: w.sellPoints,
	}
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
			return err
		}
		s := l.Status()
		loopers = append(loopers, s.UID)
	}
	gv := &State{
		ProductID:  w.productID,
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

func Load(ctx context.Context, uid string, r kv.Reader) (*Waller, error) {
	gv, err := kvutil.Get[State](ctx, r, uid)
	if err != nil {
		return nil, err
	}
	var loopers []*looper.Looper
	for _, id := range gv.Loopers {
		v, err := looper.Load(ctx, id, r)
		if err != nil {
			return nil, err
		}
		loopers = append(loopers, v)
	}
	w := &Waller{
		key:        uid,
		productID:  gv.ProductID,
		loopers:    loopers,
		buyPoints:  gv.BuyPoints,
		sellPoints: gv.SellPoints,
	}
	if err := w.check(); err != nil {
		return nil, err
	}
	return w, nil
}
