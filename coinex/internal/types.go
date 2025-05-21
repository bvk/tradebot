// Copyright (c) 2025 BVK Chaitanya

package internal

import "github.com/shopspring/decimal"

type GetMarketStatusResponse struct {
	Code int `json:"code"`

	Message string `json:"message"`

	Data []*MarketStatus `json:"data"`
}

type MarketStatus struct {
	Market string `json:"market"`
	Status string `json:"status"`

	IsAMMAvailable              bool `json:"is_amm_available"`
	IsMarginAvailable           bool `json:"is_margin_available"`
	IsPreMarketTradingAvailable bool `json:"is_pre_market_trading_available"`
	IsAPITradingAvailable       bool `json:"is_api_trading_available"`

	MakerFeeRate string `json:"maker_fee_rate"`
	TakerFeeRate string `json:"taker_fee_rate"`

	MinAmount decimal.Decimal `json:"min_amount"` // Min. transaction volume

	BaseCurrency  string `json:"base_ccy"`
	BasePrecision int    `json:"base_ccy_precision"`

	QuoteCurrency  string `json:"quote_ccy"`
	QuotePrecision int    `json:"quote_ccy_precision"`
}
