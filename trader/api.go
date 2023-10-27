// Copyright (c) 2023 BVK Chaitanya

package trader

import (
	"github.com/shopspring/decimal"
)

type LimitBuyRequest struct {
	Product string

	BuySize decimal.Decimal

	BuyPrice decimal.Decimal

	BuyCancelPrice decimal.Decimal
}

type LimitBuyResponse struct {
	UID string
}

type LimitSellRequest struct {
	Product string

	SellSize decimal.Decimal

	SellPrice decimal.Decimal

	SellCancelPrice decimal.Decimal
}

type LimitSellResponse struct {
	UID string
}

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

	BuySize decimal.Decimal

	BuyPrice decimal.Decimal

	BuyCancelPrice decimal.Decimal

	SellSize decimal.Decimal

	SellPrice decimal.Decimal

	SellCancelPrice decimal.Decimal
}

type LoopResponse struct {
	UID string
}

type WallRequest struct {
	Product string
}

type WallResponse struct {
	UID string
}
