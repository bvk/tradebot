// Copyright (c) 2025 BVK Chaitanya

package internal

import "github.com/shopspring/decimal"

type ExchangeError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type GetExchangeInfoResponse struct {
	TimeZone       string        `json:"timeZone"`
	ServerUnixTime int64         `json:"serverTime"`
	SymbolInfoList []*SymbolInfo `json:"symbols"`
}

type SymbolInfo struct {
	Symbol string `json:"symbol"`
	Status string `json:"status"`

	OrderTypes []string `json:"orderTypes"`

	BaseAsset           string `json:"baseAsset"`
	BaseAssetPrecision  int    `json:"baseAssetPrecision"`
	QuoteAsset          string `json:"quoteAsset"`
	Quoteassetprecision int    `json:"quoteAssetPrecision"`

	IsSpotTradingAllowed bool `json:"isSpotTradingAllowed"`

	Filters []*Filter `json:"filters"`
}

type Filter struct {
	FilterType string          `json:"filterType"`
	MinPrice   decimal.Decimal `json:"minPrice"`
	TickSize   decimal.Decimal `json:"tickSize"`
}

// type GetFundDetailsResponse []*FundDetail

// type FundDetail struct {
// 	Action            string          `json:"action"`
// 	Balance           []*Balance      `json:"balance"`
// 	CreatedAt         int64           `json:"created_at"`
// 	Email             string          `json:"email"`
// 	EmailVerification string          `json:"emailVerification"`
// 	Portfolio         decimal.Decimal `json:"portfolio"`
// 	Status            string          `json:"status"`
// }

// type Balance struct {
// 	Symbol  string          `json:"symbol"`
// 	Balance decimal.Decimal `json:"balance"`
// }

type GetFundsResponse []*Fund

type Fund struct {
	Asset       string          `json:"asset"`
	Free        decimal.Decimal `json:"free"`
	Locked      decimal.Decimal `json:"locked"`
	ReservedFee decimal.Decimal `json:"reservedFee"`
}
