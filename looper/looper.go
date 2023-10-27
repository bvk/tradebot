// Copyright (c) 2023 BVK Chaitanya

package looper

import (
	"bytes"
	"context"
	"encoding/gob"
	"fmt"
	"path"

	"github.com/bvkgo/kv"
	"github.com/bvkgo/tradebot/exchange"
	"github.com/bvkgo/tradebot/kvutil"
	"github.com/bvkgo/tradebot/limiter"
	"github.com/bvkgo/tradebot/point"
)

type Looper struct {
	product exchange.Product

	key string

	buyPoint  point.Point
	sellPoint point.Point

	buys  []*limiter.Limiter
	sells []*limiter.Limiter
}

type State struct {
	ProductID string
	Limiters  []string
	BuyPoint  point.Point
	SellPoint point.Point
}

type Status struct {
	UID string

	ProductID string

	BuyPoint  point.Point
	SellPoint point.Point

	NumBuys  int
	NumSells int
}

func New(uid string, product exchange.Product, buy, sell *point.Point) (*Looper, error) {
	v := &Looper{
		product:   product,
		key:       uid,
		buyPoint:  *buy,
		sellPoint: *sell,
	}
	if err := v.check(); err != nil {
		return nil, err
	}
	return v, nil
}

func (v *Looper) check() error {
	if len(v.key) == 0 || !path.IsAbs(v.key) {
		return fmt.Errorf("looper uid/key %q is invalid", v.key)
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
	return nil
}

func (v *Looper) Status() *Status {
	return &Status{
		UID:       v.key,
		ProductID: v.product.ID(),
		BuyPoint:  v.buyPoint,
		SellPoint: v.sellPoint,
		NumBuys:   len(v.buys), // FIXME: Remove the incomplete ones?
		NumSells:  len(v.sells),
	}
}

func (v *Looper) Run(ctx context.Context, db kv.Database) error {
	for ctx.Err() == nil {
		if err := v.limitBuy(ctx, db); err != nil {
			return err
		}

		if err := v.limitSell(ctx, db); err != nil {
			return err
		}
	}
	return nil
}

func (v *Looper) limitBuy(ctx context.Context, db kv.Database) error {
	uid := path.Join(v.key, fmt.Sprintf("buy-%06d", len(v.buys)))
	b, err := limiter.New(uid, v.product, &v.buyPoint)
	if err != nil {
		return err
	}
	v.buys = append(v.buys, b)
	if err := kv.WithTransaction(ctx, db, v.Save); err != nil {
		return err
	}
	if err := b.Run(ctx, db); err != nil {
		return err
	}
	return nil
}

func (v *Looper) limitSell(ctx context.Context, db kv.Database) error {
	uid := path.Join(v.key, fmt.Sprintf("sell-%06d", len(v.buys)))
	s, err := limiter.New(uid, v.product, &v.sellPoint)
	if err != nil {
		return err
	}
	v.sells = append(v.sells, s)
	if err := kv.WithTransaction(ctx, db, v.Save); err != nil {
		return err
	}
	if err := s.Run(ctx, db); err != nil {
		return err
	}
	return nil
}

func (v *Looper) Save(ctx context.Context, tx kv.Transaction) error {
	var limiters []string
	// TODO: We can avoid saving already completed limiters repeatedly.
	for _, b := range v.buys {
		if err := b.Save(ctx, tx); err != nil {
			return err
		}
		s := b.Status()
		limiters = append(limiters, s.UID)
	}
	for _, s := range v.sells {
		if err := s.Save(ctx, tx); err != nil {
			return err
		}
		ss := s.Status()
		limiters = append(limiters, ss.UID)
	}
	gv := &State{
		ProductID: v.product.ID(),
		Limiters:  limiters,
		BuyPoint:  v.buyPoint,
		SellPoint: v.sellPoint,
	}
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(gv); err != nil {
		return err
	}
	return tx.Set(ctx, v.key, &buf)
}

func Load(ctx context.Context, uid string, r kv.Reader, pmap map[string]exchange.Product) (*Looper, error) {
	gv, err := kvutil.Get[State](ctx, r, uid)
	if err != nil {
		return nil, err
	}
	product, ok := pmap[gv.ProductID]
	if !ok {
		return nil, fmt.Errorf("product %q not found", gv.ProductID)
	}
	var buys, sells []*limiter.Limiter
	for _, id := range gv.Limiters {
		v, err := limiter.Load(ctx, id, r, pmap)
		if err != nil {
			return nil, err
		}
		if v.Side() == "BUY" {
			buys = append(buys, v)
			continue
		}
		if v.Side() == "SELL" {
			sells = append(sells, v)
			continue
		}
		return nil, fmt.Errorf("unexpected limiter side %q", v.Side())
	}

	v := &Looper{
		key:       uid,
		product:   product,
		buys:      buys,
		sells:     sells,
		buyPoint:  gv.BuyPoint,
		sellPoint: gv.SellPoint,
	}
	if err := v.check(); err != nil {
		return nil, err
	}
	return v, nil
}
