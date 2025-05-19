// Copyright (c) 2025 BVK Chaitanya

package internal

import (
	"fmt"

	"github.com/shopspring/decimal"
)

type ListMarketsResponse []string

type ListMarketDetailsResponse []*MarketDetailsItem

/*
[

	{
	  "coindcx_name": "SNMBTC",
	  "base_currency_short_name": "BTC",
	  "target_currency_short_name": "SNM",
	  "target_currency_name": "Sonm",
	  "base_currency_name": "Bitcoin",
	  "min_quantity": 1,
	  "max_quantity": 90000000,
	  "min_price": 5.66e-7,
	  "max_price": 0.0000566,
	  "min_notional": 0.001,
	  "base_currency_precision": 8,
	  "target_currency_precision": 0,
	  "step": 1,
	  "order_types": [ "take_profit", "stop_limit", "market_order", "limit_order" ],
	  "symbol": "SNMBTC",
	  "ecode": "B",
	  "max_leverage": 3,
	  "max_leverage_short": null,
	  "pair": "B-SNM_BTC",
	  "status": "active"
	}

]
*/
type MarketDetailsItem struct {
	Name string `json:"coindcx_name"`

	BaseID   string `json:"base_currency_short_name"`
	BaseName string `json:"base_currency_name"`

	TargetID   string `json:"target_currency_short_name"`
	TargetName string `json:"target_currency_name"`

	MinQuantity decimal.Decimal `json:"min_quantity"`
	MaxQuantity decimal.Decimal `json:"max_quantity"`

	MinPrice    decimal.Decimal `json:"min_price"`
	MaxPrice    decimal.Decimal `json:"max_price"`
	MinNotional decimal.Decimal `json:"min_notional"`

	BasePrecision   int `json:"base_currency_precision"`
	TargetPrecision int `json:"target_currency_precision"`

	Step decimal.Decimal `json:"step"`

	OrderTypes []string `json:"order_types"`

	MarketSymbol string `json:"symbol"`

	Ecode       string          `json:"ecode"`
	MaxLeverage decimal.Decimal `json:"max_leverage"`
	Pair        string          `json:"pair"`
	// MaxLeverageShort null? `json:"max_leverage_short"`

	Status string `json:"status"`
}

// Check *MUST* validate all important fields we need in the response.
func (v *MarketDetailsItem) Check() error {
	if v.MarketSymbol == "" {
		return fmt.Errorf("market symbol cannot be empty")
	}
	return nil
}

type AnySignedRequest struct {
	UnixMilli int64 `json:"timestamp"`
}

type GetBalancesResponse []*UserBalance

type UserBalance struct {
	CurrencyID    string          `json:"currency"`
	Balance       decimal.Decimal `json:"balance"`
	LockedBalance decimal.Decimal `json:"locked_balance"`
}

type GetUsersInfoResponse UserInfo

type UserInfo struct {
	ID           string `json:"coindcx_id"`
	FirstName    string `json:"first_name"`
	LastName     string `json:"last_name"`
	MobileNumber string `json:"mobile_number"`
	Email        string `json:"email"`
}
