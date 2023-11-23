// Copyright (c) 2023 BVK Chaitanya

package db

import (
	"fmt"

	"github.com/bvk/tradebot/gobs"
)

func TypeNameValue(typename string) (any, error) {
	var v any
	switch typename {
	case "TraderJobState":
		v = new(gobs.TraderJobState)
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
	case "TraderState":
		v = new(gobs.TraderState)
	default:
		return nil, fmt.Errorf("unsupported type name %q", typename)
	}
	return v, nil
}
