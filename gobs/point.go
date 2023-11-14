// Copyright (c) 2023 BVK Chaitanya

package gobs

import "github.com/shopspring/decimal"

type Point struct {
	Size   decimal.Decimal
	Price  decimal.Decimal
	Cancel decimal.Decimal
}

type Pair struct {
	Buy  Point
	Sell Point
}
