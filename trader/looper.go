// Copyright (c) 2023 BVK Chaitanya

package trader

import (
	"context"
	"fmt"

	"github.com/bvkgo/tradebot/exchange"
	"github.com/shopspring/decimal"
)

type Looper struct {
	product exchange.Product

	taskID string

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

	buys  []*LimitBuy
	sells []*LimitSell
}

func NewLooper(product exchange.Product, taskID string, buySize, buyPrice, buyCancelPrice, sellSize, sellPrice, sellCancelPrice decimal.Decimal) (*Looper, error) {
	v := &Looper{
		product:         product,
		taskID:          taskID,
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

func (v *Looper) Run(ctx context.Context) error {
	for ctx.Err() == nil {
		if err := v.limitBuy(ctx); err != nil {
			return err
		}

		if err := v.limitSell(ctx); err != nil {
			return err
		}
	}
	return nil
}

func (v *Looper) limitBuy(ctx context.Context) error {
	b := NewLimitBuy(v.product, v.taskID, v.buyPrice, v.buyCancelPrice, v.buySize)
	if err := b.Run(ctx); err != nil {
		return err
	}
	v.buys = append(v.buys, b)
	return nil
}

func (v *Looper) limitSell(ctx context.Context) error {
	s := NewLimitSell(v.product, v.taskID, v.sellPrice, v.sellCancelPrice, v.sellSize)
	if err := s.Run(ctx); err != nil {
		return err
	}
	v.sells = append(v.sells, s)
	return nil
}
