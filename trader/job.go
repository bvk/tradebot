// Copyright (c) 2023 BVK Chaitanya

package trader

import (
	"context"

	"github.com/bvkgo/kv"
	"github.com/shopspring/decimal"
)

type Job interface {
	UID() string
	ProductID() string
	ExchangeName() string

	SoldValue() decimal.Decimal
	BoughtValue() decimal.Decimal

	Save(context.Context, kv.ReadWriter) error

	Run(context.Context, *Runtime) error
}
