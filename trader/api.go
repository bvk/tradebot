// Copyright (c) 2023 BVK Chaitanya

package trader

import "github.com/shopspring/decimal"

type LimitBuyRequest struct {
	Product string

	BuySize decimal.Decimal

	BuyPrice decimal.Decimal

	BuyCancelPrice decimal.Decimal
}

type LimitBuyResponse struct {
	TaskID string
}

type LimitSellRequest struct {
	Product string

	SellSize decimal.Decimal

	SellPrice decimal.Decimal

	SellCancelPrice decimal.Decimal
}

type LimitSellResponse struct {
	TaskID string
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
	TaskID string
}

type WallRequest struct {
	Product string
}

type WallResponse struct {
	TaskID string
}
