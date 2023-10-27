// Copyright (c) 2023 BVK Chaitanya

package trader

import (
	"bytes"
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

	key string

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
		key:             uid,
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
	if len(v.key) == 0 || !path.IsAbs(v.key) {
		return fmt.Errorf("looper uid/key %q is invalid", v.key)
	}
	if v.buyPrice.GreaterThanOrEqual(v.buyCancelPrice) {
		return fmt.Errorf("buy-cancel price must be higher than buy price")
	}
	if v.sellPrice.LessThanOrEqual(v.sellCancelPrice) {
		fmt.Errorf("sell-cancel price must be lower than the sell-price")
	}
	return nil
}

func (v *Looper) UID() string {
	return v.key
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
	b, err := NewLimiter(uid, v.product, v.buySize, v.buyPrice, v.buyCancelPrice)
	if err != nil {
		return err
	}
	v.buys = append(v.buys, b)
	if err := kv.WithTransaction(ctx, db, v.save); err != nil {
		return err
	}
	if err := b.Run(ctx, db); err != nil {
		return err
	}
	return nil
}

func (v *Looper) limitSell(ctx context.Context, db kv.Database) error {
	uid := path.Join(v.key, fmt.Sprintf("sell-%06d", len(v.buys)))
	s, err := NewLimiter(uid, v.product, v.sellSize, v.sellPrice, v.sellCancelPrice)
	if err != nil {
		return err
	}
	v.sells = append(v.sells, s)
	if err := kv.WithTransaction(ctx, db, v.save); err != nil {
		return err
	}
	if err := s.Run(ctx, db); err != nil {
		return err
	}
	return nil
}

type gobLooper struct {
	ProductID       string
	Limiters        []string
	BuySize         decimal.Decimal
	BuyPrice        decimal.Decimal
	BuyCancelPrice  decimal.Decimal
	SellSize        decimal.Decimal
	SellPrice       decimal.Decimal
	SellCancelPrice decimal.Decimal
}

func (v *Looper) save(ctx context.Context, tx kv.Transaction) error {
	var limiters []string
	// TODO: We can avoid saving already completed limiters repeatedly.
	for _, b := range v.buys {
		if err := b.save(ctx, tx); err != nil {
			return err
		}
		limiters = append(limiters, b.UID())
	}
	for _, s := range v.sells {
		if err := s.save(ctx, tx); err != nil {
			return err
		}
		limiters = append(limiters, s.UID())
	}
	gv := &gobLooper{
		ProductID:       v.product.ID(),
		Limiters:        limiters,
		BuySize:         v.buySize,
		BuyPrice:        v.buyPrice,
		BuyCancelPrice:  v.buyCancelPrice,
		SellSize:        v.sellSize,
		SellPrice:       v.sellPrice,
		SellCancelPrice: v.sellCancelPrice,
	}
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(gv); err != nil {
		return err
	}
	return tx.Set(ctx, v.key, &buf)
}

func LoadLooper(ctx context.Context, uid string, db kv.Database, pmap map[string]exchange.Product) (*Looper, error) {
	gv, err := kvGet[gobLooper](ctx, db, uid)
	if err != nil {
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
		key:             uid,
		product:         product,
		buys:            buys,
		sells:           sells,
		buySize:         gv.BuySize,
		buyPrice:        gv.BuyPrice,
		buyCancelPrice:  gv.BuyCancelPrice,
		sellSize:        gv.SellSize,
		sellPrice:       gv.SellPrice,
		sellCancelPrice: gv.SellCancelPrice,
	}
	if err := v.check(); err != nil {
		return nil, err
	}
	return v, nil
}
