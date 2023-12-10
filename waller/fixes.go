// Copyright (c) 2023 BVK Chaitanya

package waller

import (
	"context"
	"fmt"

	"github.com/bvk/tradebot/looper"
	"github.com/shopspring/decimal"
)

func FixCancelOffset(ctx context.Context, w *Waller, offset decimal.Decimal) error {
	for _, p := range w.pairs {
		p.Buy.Cancel = p.Buy.Price.Add(offset)
		p.Sell.Cancel = p.Sell.Price.Sub(offset)
		if err := p.Check(); err != nil {
			return fmt.Errorf("could not adjust cancel price for %v: %w", p, err)
		}
	}
	for _, l := range w.loopers {
		if err := looper.FixCancelOffset(ctx, l, offset); err != nil {
			return fmt.Errorf("could not fix cancel offset for looper %q: %w", l.UID(), err)
		}
	}
	return nil
}
