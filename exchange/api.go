// Copyright (c) 2023 BVK Chaitanya

package exchange

import (
	"context"
	"time"

	"github.com/shopspring/decimal"
)

type OrderID string

type Order struct {
	OrderID OrderID

	ClientOrderID string

	Side string

	CreatedTime time.Time

	Fee         decimal.Decimal
	FilledSize  decimal.Decimal
	FilledPrice decimal.Decimal

	Status string
}

type RemoteTime time.Time

type Ticker struct {
	Timestamp RemoteTime
	Price     decimal.Decimal
}

type Product interface {
	Price() decimal.Decimal

	Ticker() <-chan *Ticker

	LimitBuy(ctx context.Context, clientOrderID string, size, price decimal.Decimal) (OrderID, error)
	LimitSell(ctx context.Context, clientOrderID string, size, price decimal.Decimal) (OrderID, error)

	Get(ctx context.Context, id OrderID) (*Order, error)
	List(ctx context.Context) ([]*Order, error)

	WaitForDone(ctx context.Context, id OrderID) error
}
