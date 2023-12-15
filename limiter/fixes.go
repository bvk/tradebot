// Copyright (c) 2023 BVK Chaitanya

package limiter

import (
	"context"
	"fmt"
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
