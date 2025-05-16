// Copyright (c) 2023 BVK Chaitanya

package waller

import (
	"context"
	"fmt"
	"os"
	"strings"

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

// SwitchToUSD converts a waller job that is originally using USDC base type to
// USD. Job must be PAUSED before we can apply this change.
func SwitchToUSD(ctx context.Context, w *Waller) error {
	if !strings.HasSuffix(w.productID, "-USDC") {
		return fmt.Errorf("waller product %q is not using USDC: %w", w.productID, os.ErrInvalid)
	}
	for _, loop := range w.loopers {
		if err := looper.SwitchToUSD(ctx, loop); err != nil {
			return err
		}
	}
	w.productID = strings.TrimSuffix(w.productID, "-USDC") + "-USD"
	return nil
}
