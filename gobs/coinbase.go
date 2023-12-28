// Copyright (c) 2023 BVK Chaitanya

package gobs

import "encoding/json"

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
