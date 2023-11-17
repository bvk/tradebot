// Copyright (c) 2023 BVK Chaitanya

package api

import "github.com/shopspring/decimal"

const LimitPath = "/trader/limit"

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
