// Copyright (c) 2023 BVK Chaitanya

package waller

import (
	"sort"

	"github.com/bvk/tradebot/point"
	"github.com/shopspring/decimal"
)

type Analysis struct {
	pairs  []*point.Pair
	feePct float64
}

func Analyze(pairs []*point.Pair, feePct float64) *Analysis {
	ps := make([]*point.Pair, len(pairs))
	for i, p := range pairs {
		ps[i] = &point.Pair{Buy: p.Buy, Sell: p.Sell}
	}
	sort.Slice(ps, func(i, j int) bool {
		return ps[i].Buy.Price.LessThan(ps[j].Buy.Price)
	})

	return &Analysis{
		pairs:  ps,
		feePct: feePct,
	}
}

func (a *Analysis) NumPairs() int {
	return len(a.pairs)
}

func (a *Analysis) FeePct() float64 {
	return a.feePct
}

func (a *Analysis) Budget() decimal.Decimal {
	var sum decimal.Decimal
	for _, pair := range a.pairs {
		sum = sum.Add(pair.Buy.Value())
		sum = sum.Add(pair.FeesAt(a.feePct))
	}
	return sum
}

func (a *Analysis) MinProfitMargin() decimal.Decimal {
	return a.pairs[0].ValueMargin().Sub(a.pairs[0].FeesAt(a.feePct))
}

func (a *Analysis) AvgProfitMargin() decimal.Decimal {
	var sum decimal.Decimal
	for _, pair := range a.pairs {
		sum = sum.Add(pair.ValueMargin())
		sum = sum.Sub(pair.FeesAt(a.feePct))
	}
	return sum.Div(decimal.NewFromInt(int64(len(a.pairs))))
}

func (a *Analysis) MaxProfitMargin() decimal.Decimal {
	last := a.pairs[len(a.pairs)-1]
	return last.ValueMargin().Sub(last.FeesAt(a.feePct))
}

func (a *Analysis) MinPriceMargin() decimal.Decimal {
	return a.pairs[0].PriceMargin()
}

func (a *Analysis) AvgPriceMargin() decimal.Decimal {
	var sum decimal.Decimal
	for _, pair := range a.pairs {
		sum = sum.Add(pair.PriceMargin())
	}
	return sum.Div(decimal.NewFromInt(int64(len(a.pairs))))
}

func (a *Analysis) MaxPriceMargin() decimal.Decimal {
	return a.pairs[len(a.pairs)-1].PriceMargin()
}

func (a *Analysis) MinLoopFee() decimal.Decimal {
	return a.pairs[0].FeesAt(a.feePct)
}

func (a *Analysis) AvgLoopFee() decimal.Decimal {
	var sum decimal.Decimal
	for _, pair := range a.pairs {
		sum = sum.Add(pair.FeesAt(a.feePct))
	}
	return sum.Div(decimal.NewFromInt(int64(len(a.pairs))))
}

func (a *Analysis) MaxLoopFee() decimal.Decimal {
	return a.pairs[len(a.pairs)-1].FeesAt(a.feePct)
}

func (a *Analysis) ProfitGoalForReturnRate(targetPct float64) decimal.Decimal {
	budget := a.Budget()
	target := decimal.NewFromFloat(targetPct)

	// perYear = budget * targetPct / 100
	return budget.Mul(target.Div(decimal.NewFromInt(100)))
}

func (a *Analysis) NumSellsForReturnRate(targetPct float64) int {
	profitPerYear := a.ProfitGoalForReturnRate(targetPct)

	// nsells = profitPerYear / AvgProfitMargin
	nsells := profitPerYear.Div(a.AvgProfitMargin()).Ceil()
	return int(nsells.BigInt().Int64())
}

func (a *Analysis) ReturnRateForNumSells(nsells int) decimal.Decimal {
	profit := a.AvgProfitMargin().Mul(decimal.NewFromInt(int64(nsells)))
	// returnRate = profit * 100 / budget
	return profit.Mul(decimal.NewFromInt(100)).Div(a.Budget())
}

func (a *Analysis) LockinAt(tickerPrice decimal.Decimal) decimal.Decimal {
	var sum decimal.Decimal
	for _, pair := range a.pairs {
		if pair.Buy.Price.LessThan(tickerPrice) {
			sum = sum.Add(pair.Buy.Value())
			sum = sum.Add(pair.FeesAt(a.feePct))
		}
	}
	return sum
}
