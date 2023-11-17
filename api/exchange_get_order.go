// Copyright (c) 2023 BVK Chaitanya

package api

import (
	"time"

	"github.com/shopspring/decimal"
)

const ExchangeGetOrderPath = "/exchange/get-order"

type ExchangeGetOrderRequest struct {
	Name string

	OrderID string
}

type ExchangeGetOrderResponse struct {
	Error string

	OrderID       string
	ClientOrderID string
	Side          string
	CreateTime    time.Time
	Fee           decimal.Decimal
	FilledSize    decimal.Decimal
	FilledPrice   decimal.Decimal
	Status        string
	Done          bool
	DoneReason    string
}
