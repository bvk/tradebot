// Copyright (c) 2023 BVK Chaitanya

package trader

import (
	"github.com/bvk/tradebot/gobs"
	"github.com/shopspring/decimal"
)

func filledSize(vs []*gobs.Order) decimal.Decimal {
	var sum decimal.Decimal
	for _, v := range vs {
		sum = sum.Add(v.FilledSize)
	}
	return sum
}

func filledValue(vs []*gobs.Order) decimal.Decimal {
	var sum decimal.Decimal
	for _, v := range vs {
		sum = sum.Add(v.FilledSize.Mul(v.FilledPrice))
	}
	return sum
}

func filledFee(vs []*gobs.Order) decimal.Decimal {
	var sum decimal.Decimal
	for _, v := range vs {
		sum = sum.Add(v.FilledFee)
	}
	return sum
}
