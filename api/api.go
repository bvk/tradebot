// Copyright (c) 2023 BVK Chaitanya

package api

import (
	"github.com/bvk/tradebot/point"
	"github.com/shopspring/decimal"
)

type LimitRequest struct {
	Product string

	Size decimal.Decimal

	Price decimal.Decimal

	CancelPrice decimal.Decimal
}

type LimitResponse struct {
	UID string

	Side string
}

type LoopRequest struct {
	Product string

	Buy  point.Point
	Sell point.Point
}

type LoopResponse struct {
	UID string
}

type WallRequest struct {
	Product string

	BuySellPoints [][2]*point.Point
}

type WallResponse struct {
	UID string
}

type ListRequest struct {
}

type ListResponseItem struct {
	UID   string
	Type  string
	State string
}

type ListResponse struct {
	Jobs []*ListResponseItem
}

type PauseRequest struct {
	UID string
}
type PauseResponse struct {
	FinalState string
}

type CancelRequest struct {
	UID string
}
type CancelResponse struct {
	FinalState string
}

type ResumeRequest struct {
	UID string
}
type ResumeResponse struct {
	FinalState string
}
