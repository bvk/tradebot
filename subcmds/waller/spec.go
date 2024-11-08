// Copyright (c) 2023 BVK Chaitanya

package waller

import (
	"flag"
	"fmt"
	"log"
	"sort"

	"github.com/bvk/tradebot/point"
	"github.com/shopspring/decimal"
)

var d100 = decimal.NewFromInt(100)

type Spec struct {
	feePercentage float64

	beginPriceRange float64
	endPriceRange   float64

	buyInterval    float64
	buyIntervalPct float64

	profitMargin    float64
	profitMarginPct float64

	buySize  float64
	sellSize float64

	cancelOffsetPct float64
	cancelOffset    float64

	pairs []*point.Pair
}

func (s *Spec) SetFlags(fset *flag.FlagSet) {
	fset.Float64Var(&s.beginPriceRange, "begin-price", 0, "begin price for the trading price range")
	fset.Float64Var(&s.endPriceRange, "end-price", 0, "end price for the trading price range")
	fset.Float64Var(&s.buyInterval, "buy-interval", 0, "interval between successive buy price points")
	fset.Float64Var(&s.buyIntervalPct, "buy-interval-pct", 0, "buy-interval as pct of the previous buy point")
	fset.Float64Var(&s.profitMargin, "profit-margin", 0, "wanted profit to determine sell price point")
	fset.Float64Var(&s.profitMarginPct, "profit-margin-pct", 0, "wanted profit as a percentage of the buy point")
	fset.Float64Var(&s.buySize, "buy-size", 0, "asset buy-size for the trade")
	fset.Float64Var(&s.sellSize, "sell-size", 0, "asset sell-size for the trade")
	fset.Float64Var(&s.cancelOffsetPct, "cancel-offset-pct", 5, "cancel-at price as pct of middle of the price range")
	fset.Float64Var(&s.feePercentage, "fee-pct", 0.25, "exchange fee percentage to adjust sell margin")
}

func (s *Spec) BuySellPairs() []*point.Pair {
	return s.pairs
}

func (s *Spec) setDefaults() {
	if s.sellSize == 0 {
		s.sellSize = s.buySize
	}
	// Calculate cancel-offset as 5% away from the buy/sell prices.
	if s.cancelOffset == 0 {
		mid := decimal.NewFromFloat((s.beginPriceRange + s.endPriceRange) / 2)
		s.cancelOffset, _ = mid.Mul(decimal.NewFromFloat(s.cancelOffsetPct)).Div(decimal.NewFromInt(100)).Float64()
	}
}

func (s *Spec) Check() error {
	s.setDefaults()

	if s.beginPriceRange <= 0 || s.endPriceRange <= 0 {
		return fmt.Errorf("begin/end price ranges cannot be zero or negative")
	}
	if s.buySize <= 0 {
		return fmt.Errorf("buy size cannot be zero or negative")
	}
	if s.buyInterval <= 0 && s.buyIntervalPct <= 0 {
		return fmt.Errorf("buy interval cannot be zero or negative")
	}
	if s.buyInterval > 0 && s.buyIntervalPct > 0 {
		return fmt.Errorf("only one of buy interval and buy interval percent can be positive")
	}

	if s.cancelOffset <= 0 {
		return fmt.Errorf("buy/sell cancel offsets cannot be zero or negative")
	}

	if s.profitMargin <= 0 && s.profitMarginPct <= 0 {
		return fmt.Errorf("one of profit margin or profit margin percent must be given")
	}
	if s.profitMargin > 0 && s.profitMarginPct > 0 {
		return fmt.Errorf("only one of profit margin and profit margin percent can be positive")
	}

	if s.endPriceRange <= s.beginPriceRange {
		return fmt.Errorf("end price range cannot be lower or equal to the begin price")
	}
	if s.sellSize > 0 && s.sellSize != s.buySize {
		return fmt.Errorf("sell size must always be equal to the buy size")
	}
	if s.feePercentage < 0 || s.feePercentage >= 100 {
		return fmt.Errorf("fee percentage should be in between 0-100")
	}

	if s.profitMargin > 0 {
		pairs := fixedProfitPairs(s)
		if pairs == nil || len(pairs) == 0 {
			return fmt.Errorf("could not create fixed profit margin based buy/sell pairs")
		}
		s.pairs = pairs
	}
	if s.profitMarginPct > 0 {
		pairs := percentProfitPairs(s)
		if pairs == nil || len(pairs) == 0 {
			return fmt.Errorf("could not create percentage profit buy/sell pairs")
		}
		s.pairs = pairs
	}

	return nil
}

func (s *Spec) buyIntervalSize(p decimal.Decimal) decimal.Decimal {
	if s.buyIntervalPct == 0 {
		return decimal.NewFromFloat(s.buyInterval)
	}
	return p.Mul(decimal.NewFromFloat(s.buyIntervalPct).Div(d100))
}

func fixedProfitPairs(s *Spec) []*point.Pair {
	var pairs []*point.Pair
	beginPrice := decimal.NewFromFloat(s.beginPriceRange)
	endPrice := decimal.NewFromFloat(s.endPriceRange)
	cancelOffset := decimal.NewFromFloat(s.cancelOffset)
	for price := beginPrice; price.LessThan(endPrice); price = price.Add(s.buyIntervalSize(price)) {
		buy := &point.Point{
			Price:  price,
			Size:   decimal.NewFromFloat(s.buySize),
			Cancel: price.Add(cancelOffset),
		}
		if err := buy.Check(); err != nil {
			log.Fatal(err)
		}
		sell, err := point.SellPoint(buy, decimal.NewFromFloat(s.profitMargin))
		if err != nil {
			log.Fatal(err)
		}
		p := &point.Pair{Buy: *buy, Sell: *sell}
		if s.feePercentage != 0 {
			p = point.AdjustForMargin(p, s.feePercentage)
		}
		pairs = append(pairs, p)
	}
	sort.Slice(pairs, func(i, j int) bool {
		return pairs[i].Buy.Price.LessThan(pairs[j].Buy.Price)
	})
	return pairs
}

func percentProfitPairs(s *Spec) []*point.Pair {
	var pairs []*point.Pair
	beginPrice := decimal.NewFromFloat(s.beginPriceRange)
	endPrice := decimal.NewFromFloat(s.endPriceRange)
	cancelOffset := decimal.NewFromFloat(s.cancelOffset)
	for price := beginPrice; price.LessThan(endPrice); price = price.Add(s.buyIntervalSize(price)) {
		buy := &point.Point{
			Price:  price,
			Size:   decimal.NewFromFloat(s.buySize),
			Cancel: price.Add(cancelOffset),
		}
		if err := buy.Check(); err != nil {
			log.Fatal(err)
		}

		// margin := buyValue * marginPct / 100
		margin := buy.Value().Mul(decimal.NewFromFloat(s.profitMarginPct).Div(d100))
		sell, err := point.SellPoint(buy, margin)
		if err != nil {
			log.Fatal(err)
		}

		p := &point.Pair{Buy: *buy, Sell: *sell}
		if s.feePercentage != 0 {
			p = point.AdjustForMargin(p, s.feePercentage)
		}
		pairs = append(pairs, p)
	}
	sort.Slice(pairs, func(i, j int) bool {
		return pairs[i].Buy.Price.LessThan(pairs[j].Buy.Price)
	})
	return pairs
}
