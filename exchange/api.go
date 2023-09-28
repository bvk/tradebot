// Copyright (c) 2023 BVK Chaitanya

package exchange

import (
	"context"
	"time"

	"github.com/shopspring/decimal"
)

type Order struct {
	ClientOrderID string
	ServerOrderID string

	CreatedAt time.Time

	FilledSize decimal.Decimal
	FilledFee  decimal.Decimal

	Status string
}

type Product interface {
	Price() decimal.Decimal

	List(ctx context.Context) ([]*Order, error)

	Buy(ctx context.Context, clientID string, size, price decimal.Decimal) (*Order, error)
	Sell(ctx context.Context, clientID string, size, price decimal.Decimal) (*Order, error)
}
