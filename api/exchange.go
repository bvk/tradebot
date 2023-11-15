// Copyright (c) 2023 BVK Chaitanya

package api

import (
	"time"

	"github.com/bvk/tradebot/gobs"
	"github.com/shopspring/decimal"
)

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

type ExchangeGetProductRequest struct {
	ExchangeName string
	ProductID    string
}

type ExchangeGetProductResponse struct {
	Error string

	Product *gobs.Product
}

type ExchangeGetCandlesRequest struct {
	ExchangeName string
	ProductID    string

	StartTime time.Time
	EndTime   time.Time
}

type ExchangeGetCandlesResponse struct {
	Error string

	Candles []*gobs.Candle

	Continue *ExchangeGetCandlesRequest
}
