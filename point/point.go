// Copyright (c) 2023 BVK Chaitanya

package point

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/bvk/tradebot/gobs"
	"github.com/shopspring/decimal"
)

type Point gobs.Point

func (p Point) String() string {
	return fmt.Sprintf("%s:%s@%s", p.Side(), p.Size, p.Price.StringFixed(5))
}

func (p *Point) LogValue() slog.Value {
	return slog.StringValue(p.String())
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

func Equal(a, b gobs.Point) bool {
	return a.Size.Equal(b.Size) && a.Price.Equal(b.Price) && a.Cancel.Equal(b.Cancel)
}

func (p Point) Equal(v *Point) bool {
	return Equal(gobs.Point(p), gobs.Point(*v))
}

// InRange returns true if input price is within the activation price range for
// the trade point.
func (p Point) InRange(ticker decimal.Decimal) bool {
	if p.Side() == "BUY" {
		return ticker.GreaterThanOrEqual(p.Price) && ticker.LessThan(p.Cancel)
	}
	return ticker.GreaterThan(p.Cancel) && ticker.LessThanOrEqual(p.Price)
}

// Side returns "BUY" or "SELL" side for the point. Side is determined by
// comparing the point price and it's cancel price. Cancel price must be
// greater than point price for buy orders and lower than the point price for
// sell orders.
func (p *Point) Side() string {
	if p.Cancel.LessThan(p.Price) {
		return "SELL"
	}
	return "BUY"
}

// FeeAt returns the fee incurred for the buy or sell at the given fee
// percentage.
func (p *Point) FeeAt(pct float64) decimal.Decimal {
	return p.Value().Mul(decimal.NewFromFloat(pct)).Div(decimal.NewFromFloat(100))
}

// Value returns the dollar amount for point (i.e, size*price) without
// including any fee.
func (p *Point) Value() decimal.Decimal {
	return p.Size.Mul(p.Price)
}

// SellPoint returns a sell point for the input buy point with the given profit
// margin. Returned sell point uses the full size of the buy point as the sell
// size and uses the same cancel price offset as the buy point, but on the
// opposite side.
//
// Returns non-nil error if the input point is not a buy point or cancel offset
// becomes inappropriate.
func SellPoint(buy *Point, margin decimal.Decimal) (*Point, error) {
	if buy.Side() != "BUY" {
		return nil, os.ErrInvalid
	}
	sellValue := buy.Value().Add(margin)
	sellPrice := sellValue.Div(buy.Size)
	cancelOffset := buy.Cancel.Sub(buy.Price)
	sellCancel := sellPrice.Sub(cancelOffset)
	sell := &Point{
		Size:   buy.Size,
		Price:  sellPrice,
		Cancel: sellCancel,
	}
	if err := sell.Check(); err != nil {
		return nil, err
	}
	if sell.Side() != "SELL" {
		return nil, fmt.Errorf("unexpected sell point side result")
	}
	return sell, nil
}

// BuyPoint returns a buy point for the input sell point with the given profit
// margin. Returned buy point uses the same size as the sell point and same
// cancel price offset as the sell point, but on the opposite side.
//
// Returns non-nil error if the input point is not a sell point or cancel
// offset becomes inappropriate.
func BuyPoint(sell *Point, margin decimal.Decimal) (*Point, error) {
	if sell.Side() != "SELL" {
		return nil, os.ErrInvalid
	}
	buyValue := sell.Value().Sub(margin)
	buyPrice := buyValue.Div(sell.Size)
	cancelOffset := sell.Price.Sub(sell.Cancel)
	buyCancel := buyPrice.Add(cancelOffset)
	buy := &Point{
		Size:   sell.Size,
		Price:  buyPrice,
		Cancel: buyCancel,
	}
	if err := buy.Check(); err != nil {
		return nil, err
	}
	if buy.Side() != "BUY" {
		return nil, fmt.Errorf("unexpected buy point side result")
	}
	return buy, nil
}
