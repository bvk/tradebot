// Copyright (c) 2023 BVK Chaitanya

package trader

import (
	"fmt"

	"github.com/shopspring/decimal"
)

type Point struct {
	Size   decimal.Decimal
	Price  decimal.Decimal
	Cancel decimal.Decimal
}

func (p *Point) Check() error {
	if p.Size.IsZero() {
		return fmt.Errorf("size cannot be zero")
	}
	if p.Size.IsNegative() {
		return fmt.Errorf("size cannot be negative")
	}
	if p.Price.IsZero() {
		return fmt.Errorf("price cannot be zero")
	}
	if p.Price.IsNegative() {
		return fmt.Errorf("price cannot be negative")
	}
	if p.Cancel.IsZero() {
		return fmt.Errorf("cancel-price cannot be zero")
	}
	if p.Cancel.IsNegative() {
		return fmt.Errorf("cancel-price cannot be negative")
	}
	if p.Cancel.Equal(p.Price) {
		return fmt.Errorf("cancel-price cannot be equal to the price")
	}
	return nil
}

func (p *Point) Side() string {
	if p.Cancel.LessThan(p.Price) {
		return "SELL"
	}
	return "BUY"
}

func (p *Point) String() string {
	return fmt.Sprintf("%s{%s@%s/%s}", p.Side(), p.Size, p.Price, p.Cancel)
}
