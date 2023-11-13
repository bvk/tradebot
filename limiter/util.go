// Copyright (c) 2023 BVK Chaitanya

package limiter

import (
	"sort"

	"github.com/bvk/tradebot/exchange"
)

func (v *Limiter) GetOrders() []*exchange.Order {
	var orders []*exchange.Order
	for _, order := range v.orderMap {
		if order.Done && !order.FilledSize.IsZero() {
			orders = append(orders, order)
		}
	}
	sort.Slice(orders, func(i, j int) bool {
		return orders[i].CreateTime.Time.Before(orders[j].CreateTime.Time)
	})
	return orders
}
