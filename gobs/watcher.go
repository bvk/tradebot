// Copyright (c) 2025 BVK Chaitanya

package gobs

import (
	"time"

	"github.com/shopspring/decimal"
)

type WatcherState struct {
	ProductID    string
	ExchangeName string
	AccountName  string

	FeePct decimal.Decimal

	TradeLoops []*WatcherLoop

	LifetimeSummary *Summary
}

func (v *WatcherState) Upgrade() {
	if len(v.ExchangeName) == 0 {
		v.ExchangeName = "coinbase"
	}
	if len(v.AccountName) == 0 {
		v.AccountName = v.ExchangeName
	}
}

type WatcherLoop struct {
	Pair Pair

	Buys  []time.Time
	Sells []time.Time
}
