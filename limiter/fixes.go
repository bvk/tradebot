// Copyright (c) 2023 BVK Chaitanya

package limiter

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/shopspring/decimal"
)

func FixCancelOffset(ctx context.Context, l *Limiter, offset decimal.Decimal) error {
	if strings.Contains(l.UID(), "/buy-") {
		l.point.Cancel = l.point.Price.Add(offset)
	}
	if strings.Contains(l.UID(), "/sell-") {
		l.point.Cancel = l.point.Price.Sub(offset)
	}
	if err := l.check(); err != nil {
		return fmt.Errorf("could not adjust cancel price for %v: %w", l.UID(), err)
	}
	return nil
}

// SwitchToUSD converts a limiter that is originally using USDC base type to
// USD. Job must be PAUSED before we can apply this change.
func SwitchToUSD(ctx context.Context, l *Limiter) error {
	if !strings.HasSuffix(l.productID, "-USDC") {
		return fmt.Errorf("limiter product %q is not using USDC: %w", l.productID, os.ErrInvalid)
	}
	if l.PendingSize().IsZero() {
		return nil // No change is necessary.
	}
	l.productID = strings.TrimSuffix(l.productID, "-USDC") + "-USD"
	return nil
}
