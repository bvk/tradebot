// Copyright (c) 2024 BVK Chaitanya

package waller

import (
	"github.com/bvk/tradebot/point"
	"github.com/bvk/tradebot/trader"
)

// PairStatus returns trade status for a buy-sell pair. Returns nil if trading
// pair is not one of the trade pairs of the waller.
func (w *Waller) PairStatus(p *point.Pair) *trader.Status {
	for _, l := range w.loopers {
		if p.Equal(l.Pair()) {
			return l.Status()
		}
	}
	return nil
}

func (w *Waller) Status() *trader.Status {
	var ss []*trader.Status
	for _, l := range w.loopers {
		ss = append(ss, l.Status())
	}
	summary := trader.Summarize(ss, nil)
	s := &trader.Status{
		UID:          w.uid,
		ProductID:    w.productID,
		ExchangeName: w.exchangeName,
		Summary:      summary,
	}
	return s
}
