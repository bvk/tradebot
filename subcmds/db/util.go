// Copyright (c) 2023 BVK Chaitanya

package db

import (
	"fmt"

	"github.com/bvk/tradebot/gobs"
)

func TypeNameValue(typename string) (any, error) {
	var v any
	switch typename {
	case "JobData":
		v = new(gobs.JobData)
	case "LimiterState":
		v = new(gobs.LimiterState)
	case "LooperState":
		v = new(gobs.LooperState)
	case "WallerState":
		v = new(gobs.WallerState)
	case "KeyValue":
		v = new(gobs.KeyValue)
	case "NameData":
		v = new(gobs.NameData)
	case "Candles":
		v = new(gobs.Candles)
	case "ServerState":
		v = new(gobs.ServerState)
	case "CoinbaseOrders":
		v = new(gobs.CoinbaseOrders)
	default:
		return nil, fmt.Errorf("unsupported type name %q", typename)
	}
	return v, nil
}
