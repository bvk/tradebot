// Copyright (c) 2023 BVK Chaitanya

package waller

import (
	"fmt"
	"os"

	"github.com/bvk/tradebot/exchange"
	"github.com/bvk/tradebot/looper"
	"github.com/bvk/tradebot/point"
)

func (w *Waller) BuySellPairs() []*point.Pair {
	var pairs []*point.Pair
	for i := range w.buyPoints {
		p := &point.Pair{
			Buy:  *w.buyPoints[i],
			Sell: *w.sellPoints[i],
		}
		pairs = append(pairs, p)
	}
	return pairs
}

func (w *Waller) findLooper(p *point.Pair) *looper.Looper {
	for _, l := range w.loopers {
		if p.Equal(l.Pair()) {
			return l
		}
	}
	return nil
}

func (w *Waller) GetBuyOrders(p *point.Pair) ([]*exchange.Order, error) {
	loop := w.findLooper(p)
	if loop == nil {
		return nil, fmt.Errorf("could not find looper for %s: %w", p, os.ErrNotExist)
	}
	return loop.GetBuyOrders(), nil
}

func (w *Waller) GetSellOrders(p *point.Pair) ([]*exchange.Order, error) {
	loop := w.findLooper(p)
	if loop == nil {
		return nil, fmt.Errorf("could not find looper for %s: %w", p, os.ErrNotExist)
	}
	return loop.GetSellOrders(), nil
}
