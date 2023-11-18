// Copyright (c) 2025 BVK Chaitanya

package gobs

import "github.com/shopspring/decimal"

type TeaserState struct {
	ProductID    string
	ExchangeName string

	LifetimeSummary *Summary

	FeePct decimal.Decimal

	ClientIDSeed string

	TeaserLoops []*TeaserLoopData
}

type TeaserLoopData struct {
	Pair Pair

	ClientIDOffset int64

	Buys  []*Order
	Sells []*Order
}
