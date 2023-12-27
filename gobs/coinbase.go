// Copyright (c) 2023 BVK Chaitanya

package gobs

import "encoding/json"

type CoinbaseOrders struct {
	// OrderMap is a mapping from coinbase order-id to it's data in json format.
	OrderMap map[string]json.RawMessage
}

type CoinbaseCandle struct {
	UnixTime int64
	Candle   json.RawMessage
}

type CoinbaseCandles struct {
	ProductCandlesMap map[string][]*CoinbaseCandle
}
