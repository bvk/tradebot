// Copyright (c) 2025 BVK Chaitanya

package gobs

import (
	"time"

	"github.com/shopspring/decimal"
)

type WatcherState struct {
	ProductID    string
	ExchangeName string

	FeePct decimal.Decimal

	TradeLoops []*WatcherLoop

	LifetimeSummary *Summary
}

type WatcherLoop struct {
	Pair Pair

	Buys  []time.Time
	Sells []time.Time
}
