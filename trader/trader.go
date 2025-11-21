// Copyright (c) 2023 BVK Chaitanya

package trader

import (
	"context"

	"github.com/bvk/tradebot/gobs"
	"github.com/bvk/tradebot/timerange"
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
	BudgetAt(feePct decimal.Decimal) decimal.Decimal

	GetSummary(*timerange.Range) *gobs.Summary

	// SetOption updates a trader job's customize-able runtime parameters.
	// Options can only set/changed when a job is *not* running. If successful
	// change, returns an undo-value for the option. An immediate SetOption call
	// with the returned undo-value (without running the job) *must* be
	// successful -- which serves as an undo mechanism.
	SetOption(opt, val string) (string, error)
}
