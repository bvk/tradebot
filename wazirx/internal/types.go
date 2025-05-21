// Copyright (c) 2025 BVK Chaitanya

package internal

import (
	"fmt"
	"slices"

	"github.com/shopspring/decimal"
)

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

type CreateOrderRequest struct {
	ClientOrderID string

	Symbol string // Product/Market name

	Side string // BUY or SELL

	Price decimal.Decimal

	Quantity decimal.Decimal
}

func (v *CreateOrderRequest) Check() error {
	if len(v.ClientOrderID) == 0 {
		return fmt.Errorf("client order id cannot be empty")
	}
	if len(v.Symbol) == 0 {
		return fmt.Errorf("symbol cannot be empty")
	}
	if !slices.Contains([]string{"BUY", "SELL"}, v.Side) {
		return fmt.Errorf("side must be one of BUY or SELL")
	}
	if v.Price.IsZero() || v.Price.IsNegative() {
		return fmt.Errorf("price cannot be zero or negative")
	}
	if v.Quantity.IsZero() || v.Quantity.IsNegative() {
		return fmt.Errorf("quantity cannot be zero or negative")
	}
	return nil
}

type CreateOrderResponse struct {
	ID int64 `json:"id"`

	ClientOrderID string `json:"clientOrderId"`

	Symbol string `json:"symbol"`

	Price decimal.Decimal `json:"price"`

	OriginalQuantity decimal.Decimal `json:"origQty"`
	ExecutedQuantity decimal.Decimal `json:"executedQty"`

	Status    string `json:"status"`
	OrderType string `json:"type"`
	Side      string `json:"side"`

	CreatedTime int64 `json:"createdTime"`
	UpdatedTime int64 `json:"updatedTime"`
}

func (v *CreateOrderResponse) Check() error {
	if v.ID <= 0 {
		return fmt.Errorf("order id must not be empty")
	}
	if len(v.Symbol) == 0 {
		return fmt.Errorf("symbol must not be empty")
	}
	return nil
}

type GetSymbol24Response struct {
	Symbol     string          `json:"symbol"`
	BaseAsset  string          `json:"baseAsset"`
	QuoteAsset string          `json:"quoteAsset"`
	OpenPrice  decimal.Decimal `json:"openPrice"`
	LowPrice   decimal.Decimal `json:"lowPrice"`
	HighPrice  decimal.Decimal `json:"highPrice"`
	LastPrice  decimal.Decimal `json:"lastPrice"`
	Volume     decimal.Decimal `json:"volume"`
	BidPrice   decimal.Decimal `json:"bidPrice"`
	AskPrice   decimal.Decimal `json:"askPrice"`
	Timestamp  int64           `json:"at"`
}
