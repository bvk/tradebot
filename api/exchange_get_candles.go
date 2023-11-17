// Copyright (c) 2023 BVK Chaitanya

package api

import (
	"time"

	"github.com/bvk/tradebot/gobs"
)

const ExchangeGetCandlesPath = "/exchange/get-candles"

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
