// Copyright (c) 2025 BVK Chaitanya

package exchange

import (
	"github.com/bvk/tradebot/gobs"
	"github.com/shopspring/decimal"
)

type SimpleTicker struct {
	ServerTime RemoteTime
	Price      decimal.Decimal
}

func (v *SimpleTicker) PricePoint() (decimal.Decimal, gobs.RemoteTime) {
	return v.Price, gobs.RemoteTime{Time: v.ServerTime.Time}
}
