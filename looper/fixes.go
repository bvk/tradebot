// Copyright (c) 2023 BVK Chaitanya

package looper

import (
	"context"
	"fmt"

	"github.com/bvk/tradebot/limiter"
	"github.com/shopspring/decimal"
)

func FixCancelOffset(ctx context.Context, l *Looper, offset decimal.Decimal) error {
	l.buyPoint.Cancel = l.buyPoint.Price.Add(offset)
	l.sellPoint.Cancel = l.sellPoint.Price.Sub(offset)
	if err := l.check(); err != nil {
		return fmt.Errorf("could not adjust cancel price for %v: %w", l.UID(), err)
	}
	for _, b := range l.buys {
		if err := limiter.FixCancelOffset(ctx, b, offset); err != nil {
			return fmt.Errorf("could not fix cancel offset for limit buy %q: %w", b.UID(), err)
		}
	}
	for _, s := range l.sells {
		if err := limiter.FixCancelOffset(ctx, s, offset); err != nil {
			return fmt.Errorf("could not fix cancel offset for limit sell %q: %w", s.UID(), err)
		}
	}
	return nil
}
