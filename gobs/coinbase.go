// Copyright (c) 2023 BVK Chaitanya

package gobs

import "encoding/json"

type CoinbaseOrders struct {
	RawOrderMap map[string]string

	// OrderMap is a mapping from coinbase order-id to it's data in json format.
	OrderMap map[string]json.RawMessage
}
