// Copyright (c) 2025 BVK Chaitanya

package exchange

import (
	"github.com/shopspring/decimal"
)

type SimpleBalance struct {
	ServerTime  RemoteTime
	Symbol      string
	FreeBalance decimal.Decimal
}

func (v *SimpleBalance) Balance() (string, decimal.Decimal) {
	return v.Symbol, v.FreeBalance
}
