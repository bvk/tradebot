// Copyright (c) 2023 BVK Chaitanya

package trader

import (
	"context"

	"github.com/bvk/tradebot/gobs"
	"github.com/bvkgo/kv"
	"github.com/shopspring/decimal"
)

type Trader interface {
	UID() string
	ProductID() string
	ExchangeName() string

	Save(context.Context, kv.ReadWriter) error

	Run(context.Context, *Runtime) error

	// Actions returns all buy/sell actions performed by the trader.
	//
	// Typically, orders for buy actions are followed by their corresponding sell
	// action orders. However, an unsold buy may not have a matching a sell.
	Actions() []*gobs.Action

	// BudgetAt returns the total amount of value required to execute at the
	// given fee percentage.
	BudgetAt(feePct float64) decimal.Decimal

	// SetOption updates trader job's customize-able parameters.
	SetOption(opt, val string) error
}
