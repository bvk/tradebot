// Copyright (c) 2025 BVK Chaitanya

package exchange

import "github.com/shopspring/decimal"

type SimpleTicker struct {
	ServerTime RemoteTime
	Price      decimal.Decimal
}

func (v *SimpleTicker) PricePoint() (decimal.Decimal, RemoteTime) {
	return v.Price, v.ServerTime
}
