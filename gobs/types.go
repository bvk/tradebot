// Copyright (c) 2023 BVK Chaitanya

package gobs

import (
	"github.com/bvk/tradebot/exchange"
	"github.com/bvk/tradebot/job"
	"github.com/bvk/tradebot/point"
)

type LimiterState struct {
	UID               string
	ProductID         string
	Offset            uint64
	Point             point.Point
	OrderMap          map[exchange.OrderID]*exchange.Order
	ClientServerIDMap map[string]string
}

type LooperState struct {
	ProductID string
	Limiters  []string
	BuyPoint  point.Point
	SellPoint point.Point
}

type WallerState struct {
	ProductID  string
	BuyPoints  []*point.Point
	SellPoints []*point.Point
	Loopers    []string
}

type TraderJobState struct {
	State job.State

	NeedsManualResume bool
}
