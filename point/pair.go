// Copyright (c) 2023 BVK Chaitanya

package point

import (
	"fmt"
	"log"
	"log/slog"

	"github.com/bvk/tradebot/gobs"
	"github.com/shopspring/decimal"
)

type Pair struct {
	Buy, Sell Point
}

func NewPairFromGobPair(p *gobs.Pair) *Pair {
	return &Pair{
		Buy: Point{
			Size:   p.Buy.Size,
			Price:  p.Buy.Price,
			Cancel: p.Buy.Cancel,
		},
		Sell: Point{
			Size:   p.Sell.Size,
			Price:  p.Sell.Price,
			Cancel: p.Sell.Cancel,
		},
	}
}

func (p Pair) String() string {
	return fmt.Sprintf("{%s=%s}", p.Buy, p.Sell)
}

func (p Pair) LogValue() slog.Value {
	return slog.StringValue(p.String())
}

func (p *Pair) Check() error {
	if err := p.Buy.Check(); err != nil {
		return err
	}
	if err := p.Sell.Check(); err != nil {
		return err
	}
	if p.Buy.Side() != "BUY" {
		return fmt.Errorf("buy point has invalid side")
	}
	if p.Sell.Side() != "SELL" {
		return fmt.Errorf("sell point has invalid side")
	}
	if p.Sell.Size.GreaterThan(p.Buy.Size) {
		return fmt.Errorf("sell size is more than buy size")
	}
	// if p.Sell.Price.LessThan(p.Buy.Price) {
	// 	return fmt.Errorf("sell price is lower than buy price")
	// }
	return nil
}

func (p *Pair) Equal(v *Pair) bool {
	return p.Buy.Equal(&v.Buy) && p.Sell.Equal(&v.Sell)
}

// PriceMargin returns the difference between sell and buy price points.
func (p *Pair) PriceMargin() decimal.Decimal {
	return p.Sell.Price.Sub(p.Buy.Price)
}

// ValueMargin returns the difference between sell and buy values, which is
// usually the profit margin without considering any fees.
func (p *Pair) ValueMargin() decimal.Decimal {
	return p.Sell.Value().Sub(p.Buy.Value())
}

// FeesAt returns the sum of buy and sell point fees for the given fee
// percentage.
func (p *Pair) FeesAt(pct decimal.Decimal) decimal.Decimal {
	return p.Buy.FeeAt(pct).Add(p.Sell.FeeAt(pct))
}

// AdjustForMargin returns a new pair with an adjusted sell price to account
// for the given percentage of fees added to both the buy and sell points such
// that returned pair retains the same Margin as the input pair. Sell point's
// cancel price is also adjust to the same amount so that final sell point is
// consistent.
func AdjustForMargin(p *Pair, pct decimal.Decimal) *Pair {
	//
	// Given `Value = Price*Size` and `Fee = Value * pct / 100`
	//
	// SellValue - SellFee - BuyValue - BuyFee == Margin
	//
	// => SellValue - SellFee = Margin + BuyValue + BuyFee
	//
	// => SellValue - SellValue*pct/100 = Margin + BuyValue + BuyFee
	//
	// => SellValue * (1-pct/100) = Margin + BuyValue + BuyFee
	//
	// => SellValue = (Margin + BuyValue + BuyFee) / (1-pct/100)
	//

	// divisor := 1 - pct/100
	divisor := decimal.NewFromInt(1).Sub(pct.Div(decimal.NewFromInt(100)))

	sellValue := p.ValueMargin().Add(p.Buy.Value()).Add(p.Buy.FeeAt(pct)).Div(divisor)

	sellPrice := sellValue.Div(p.Sell.Size)

	diff := sellPrice.Sub(p.Sell.Price)
	if diff.IsNegative() {
		log.Fatalf("new sell price should be more than old sell price")
	}

	adjusted := &Pair{Buy: p.Buy, Sell: p.Sell}
	adjusted.Sell.Price = adjusted.Sell.Price.Add(diff)
	adjusted.Sell.Cancel = adjusted.Sell.Cancel.Add(diff)
	return adjusted
}
