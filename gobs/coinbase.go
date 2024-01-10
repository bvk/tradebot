// Copyright (c) 2023 BVK Chaitanya

package gobs

import (
	"encoding/json"
	"time"

	"github.com/shopspring/decimal"
)

type CoinbaseOrder struct {
	OrderID string
	Order   json.RawMessage
}

type CoinbaseOrderIDs struct {
	// ProductOrderIDsMap is a mapping from product id to list of order-ids that
	// have completed with a non-zero filled-size.
	ProductOrderIDsMap map[string][]string
}

type CoinbaseCandle struct {
	UnixTime int64
	Candle   json.RawMessage
}

type CoinbaseCandles struct {
	ProductCandlesMap map[string][]*CoinbaseCandle
}

type CoinbaseAccount struct {
	CurrencyID string
	Account    json.RawMessage
}

type CoinbaseAccounts struct {
	Timestamp time.Time
	Accounts  []*CoinbaseAccount
}

type CoinbaseProduct struct {
	ProductID string
	Price     decimal.Decimal
	Product   json.RawMessage
}

type CoinbaseProducts struct {
	Timestamp time.Time
	Products  []*CoinbaseProduct
}
