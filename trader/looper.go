// Copyright (c) 2023 BVK Chaitanya

package trader

import (
	"context"
	"encoding/gob"
	"fmt"
	"path"

	"github.com/bvkgo/kv"
	"github.com/bvkgo/tradebot/exchange"
	"github.com/shopspring/decimal"
)

type Looper struct {
	product exchange.Product

	uid string

	// buySize is the amount of asset to buy.
	buySize decimal.Decimal

	// sellSize is the amount of asset to sell.
	sellSize decimal.Decimal

	// buyPrice is the limit price for buy order.
	buyPrice decimal.Decimal

	// sellPrice is the limit price for sell order.
	sellPrice decimal.Decimal

	// buyCancelPrice is the ticker price limit above which buy order is canceled
	// to avoid holding up our balances.
	buyCancelPrice decimal.Decimal

	// sellCancelPrice is the ticker price limit below which sell order is
	// canceled to avoid holding up our balances.
	sellCancelPrice decimal.Decimal

	buys  []*Limiter
	sells []*Limiter
}

func NewLooper(uid string, product exchange.Product, buySize, buyPrice, buyCancelPrice, sellSize, sellPrice, sellCancelPrice decimal.Decimal) (*Looper, error) {
	v := &Looper{
		product:         product,
		uid:             uid,
		buySize:         buySize,
		buyPrice:        buyPrice,
		buyCancelPrice:  buyCancelPrice,
		sellSize:        sellSize,
		sellPrice:       sellPrice,
		sellCancelPrice: sellCancelPrice,
	}
	if err := v.check(); err != nil {
		return nil, err
	}
	return v, nil
}

func (v *Looper) check() error {
	if v.buyPrice.GreaterThanOrEqual(v.buyCancelPrice) {
		return fmt.Errorf("buy-cancel price must be higher than buy price")
	}
	if v.sellPrice.LessThanOrEqual(v.sellCancelPrice) {
		fmt.Errorf("sell-cancel price must be lower than the sell-price")
	}
	return nil
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
	uid := path.Join(v.uid, fmt.Sprintf("buy-%d", len(v.buys)))
	b, err := NewLimiter(uid, v.product, v.buySize, v.buyPrice, v.buyCancelPrice)
	if err != nil {
		return err
	}
	if err := b.Run(ctx, db); err != nil {
		return err
	}
	v.buys = append(v.buys, b)
	return nil
}

func (v *Looper) limitSell(ctx context.Context, db kv.Database) error {
	uid := path.Join(v.uid, fmt.Sprintf("sell-%d", len(v.buys)))
	s, err := NewLimiter(uid, v.product, v.sellSize, v.sellPrice, v.sellCancelPrice)
	if err != nil {
		return err
	}
	if err := s.Run(ctx, db); err != nil {
		return err
	}
	v.sells = append(v.sells, s)
	return nil
}

type gobLooper struct {
	ProductID string
	Limiters  []string
}

func LoadLooper(ctx context.Context, uid string, db kv.Database, pmap map[string]exchange.Product) (*Looper, error) {
	key := path.Join("/loopers", uid)
	buf, err := kvGet(ctx, db, key)
	if err != nil {
		return nil, err
	}
	gv := new(gobLooper)
	if err := gob.NewDecoder(buf).Decode(gv); err != nil {
		return nil, err
	}
	product, ok := pmap[gv.ProductID]
	if !ok {
		return nil, fmt.Errorf("product %q not found", gv.ProductID)
	}
	var buys, sells []*Limiter
	for _, id := range gv.Limiters {
		v, err := LoadLimiter(ctx, id, db, pmap)
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
		product: product,
	}
	return v, nil
}
