// Copyright (c) 2023 BVK Chaitanya

package looper

import (
	"github.com/bvk/tradebot/exchange"
	"github.com/bvk/tradebot/point"
)

func (v *Looper) Pair() *point.Pair {
	return &point.Pair{Buy: v.buyPoint, Sell: v.sellPoint}
}

func (v *Looper) GetBuyOrders() []*exchange.Order {
	var orders []*exchange.Order
	for _, b := range v.buys {
		orders = append(orders, b.GetOrders()...)
	}
	return orders
}

func (v *Looper) GetSellOrders() []*exchange.Order {
	var orders []*exchange.Order
	for _, s := range v.sells {
		orders = append(orders, s.GetOrders()...)
	}
	return orders
}
