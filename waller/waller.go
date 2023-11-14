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

	productID string

	buyPoints  []*point.Point
	sellPoints []*point.Point

	loopers []*looper.Looper
}

type Status struct {
	UID string

	ProductID string

	BuyPoints  []*point.Point
	SellPoints []*point.Point

	// TODO: Add more status data.
}

func New(uid string, productID string, buys, sells []*point.Point) (*Waller, error) {
	w := &Waller{
		uid:        uid,
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
	if len(w.uid) == 0 {
		return fmt.Errorf("waller uid is empty")
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
	return "waller:" + w.uid
}

func (w *Waller) Status() *Status {
	return &Status{
		UID:        w.uid,
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
			return fmt.Errorf("could not save looper state: %w", err)
		}
		s := l.Status()
		loopers = append(loopers, s.UID)
	}
	gv := &gobs.WallerState{
		V2: &gobs.WallerStateV2{
			ProductID:  w.productID,
			LooperIDs:  loopers,
			TradePairs: make([]*gobs.Pair, len(w.buyPoints)),
		},
	}
	for i := range w.buyPoints {
		gv.V2.TradePairs[i] = &gobs.Pair{
			Buy: gobs.Point{
				Size:   w.buyPoints[i].Size,
				Price:  w.buyPoints[i].Price,
				Cancel: w.buyPoints[i].Cancel,
			},
			Sell: gobs.Point{
				Size:   w.sellPoints[i].Size,
				Price:  w.sellPoints[i].Price,
				Cancel: w.sellPoints[i].Cancel,
			},
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
		v, err := looper.Load(ctx, id, r)
		if err != nil {
			return nil, err
		}
		loopers = append(loopers, v)
	}
	w := &Waller{
		uid:        uid,
		productID:  gv.V2.ProductID,
		loopers:    loopers,
		buyPoints:  make([]*point.Point, len(gv.V2.TradePairs)),
		sellPoints: make([]*point.Point, len(gv.V2.TradePairs)),
	}
	for i, pair := range gv.V2.TradePairs {
		w.buyPoints[i] = &point.Point{
			Size:   pair.Buy.Size,
			Price:  pair.Buy.Price,
			Cancel: pair.Buy.Cancel,
		}
		w.sellPoints[i] = &point.Point{
			Size:   pair.Sell.Size,
			Price:  pair.Sell.Price,
			Cancel: pair.Sell.Cancel,
		}
	}
	if err := w.check(); err != nil {
		return nil, err
	}
	return w, nil
}
