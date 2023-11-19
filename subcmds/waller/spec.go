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

type Spec struct {
	feePercentage float64

	beginPriceRange float64
	endPriceRange   float64

	buyInterval     float64
	profitMargin    float64
	profitMarginPct float64

	buySize  float64
	sellSize float64

	cancelOffset float64

	pairs []*point.Pair
}

func (s *Spec) SetFlags(fset *flag.FlagSet) {
	fset.Float64Var(&s.beginPriceRange, "begin-price", 0, "begin price for the trading price range")
	fset.Float64Var(&s.endPriceRange, "end-price", 0, "end price for the trading price range")
	fset.Float64Var(&s.buyInterval, "buy-interval", 0, "interval between successive buy price points")
	fset.Float64Var(&s.profitMargin, "profit-margin", 0, "wanted profit to determine sell price point")
	fset.Float64Var(&s.profitMarginPct, "profit-margin-pct", 0, "wanted profit as a percentage of the buy point")
	fset.Float64Var(&s.buySize, "buy-size", 0, "asset buy-size for the trade")
	fset.Float64Var(&s.sellSize, "sell-size", 0, "asset sell-size for the trade")
	fset.Float64Var(&s.cancelOffset, "cancel-offset", 50, "cancel-at price offset for the buy/sell points")
	fset.Float64Var(&s.feePercentage, "fee-pct", 0.25, "exchange fee percentage to adjust sell margin")
}

func (s *Spec) BuySellPairs() []*point.Pair {
	return s.pairs
}

func (s *Spec) setDefaults() {
}

func (s *Spec) Check() error {
	s.setDefaults()

	if s.beginPriceRange <= 0 || s.endPriceRange <= 0 {
		return fmt.Errorf("begin/end price ranges cannot be zero or negative")
	}
	if s.buySize <= 0 || s.sellSize <= 0 {
		return fmt.Errorf("buy/sell sizes cannot be zero or negative")
	}
	if s.buyInterval <= 0 {
		return fmt.Errorf("buy interval cannot be zero or negative")
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
	if diff := s.endPriceRange - s.beginPriceRange; diff <= s.buyInterval {
		return fmt.Errorf("price range %f is too small for the buy interval %f", diff, s.buyInterval)
	}
	if s.buySize < s.sellSize {
		return fmt.Errorf("buy size cannot be lesser than sell size")
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
			return fmt.Errorf("could not create profit margin percentage based buy/sell pairs")
		}
		s.pairs = pairs
	}

	return nil
}

func fixedProfitPairs(s *Spec) []*point.Pair {
	var pairs []*point.Pair
	for price := s.beginPriceRange; price < s.endPriceRange; price += s.buyInterval {
		buy := &point.Point{
			Price:  decimal.NewFromFloat(price),
			Size:   decimal.NewFromFloat(s.buySize),
			Cancel: decimal.NewFromFloat(price + s.cancelOffset),
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
	for price := s.beginPriceRange; price < s.endPriceRange; price += s.buyInterval {
		buy := &point.Point{
			Price:  decimal.NewFromFloat(price),
			Size:   decimal.NewFromFloat(s.buySize),
			Cancel: decimal.NewFromFloat(price + s.cancelOffset),
		}
		if err := buy.Check(); err != nil {
			log.Fatal(err)
		}

		// margin := buyValue * marginPct / 100
		margin := buy.Value().Mul(decimal.NewFromFloat(s.profitMarginPct).Div(decimal.NewFromInt(100)))
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
