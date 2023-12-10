// Copyright (c) 2023 BVK Chaitanya

package limiter

import (
	"context"
	"fmt"

	"github.com/shopspring/decimal"
)

func FixCancelOffset(ctx context.Context, l *Limiter, offset decimal.Decimal) error {
	l.point.Cancel = l.point.Price.Add(offset)
	l.point.Cancel = l.point.Price.Sub(offset)
	if err := l.check(); err != nil {
		return fmt.Errorf("could not adjust cancel price for %v: %w", l.UID(), err)
	}
	return nil
}
