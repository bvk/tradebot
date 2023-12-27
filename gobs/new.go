// Copyright (c) 2023 BVK Chaitanya

package gobs

import (
	"fmt"
)

func NewByTypename(typename string) (any, error) {
	var v any
	switch typename {
	case "JobData":
		v = new(JobData)
	case "LimiterState":
		v = new(LimiterState)
	case "LooperState":
		v = new(LooperState)
	case "WallerState":
		v = new(WallerState)
	case "KeyValue":
		v = new(KeyValue)
	case "NameData":
		v = new(NameData)
	case "Candles":
		v = new(Candles)
	case "ServerState":
		v = new(ServerState)
	case "CoinbaseOrders":
		v = new(CoinbaseOrders)
	case "CoinbaseCandles":
		v = new(CoinbaseCandles)
	default:
		return nil, fmt.Errorf("unsupported type name %q", typename)
	}
	return v, nil
}
