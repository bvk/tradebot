// Copyright (c) 2024 BVK Chaitanya

package looper

import (
	"log"
	"slices"

	"github.com/bvk/tradebot/gobs"
	"github.com/bvk/tradebot/trader"
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

func avgPrice(vs []*gobs.Order) decimal.Decimal {
	var sum decimal.Decimal
	for _, v := range vs {
		sum = sum.Add(v.FilledPrice)
	}
	return sum.Div(decimal.NewFromInt(int64(len(vs))))
}

func maxPrice(max decimal.Decimal, vs []*gobs.Order) decimal.Decimal {
	for _, v := range vs {
		if v.FilledPrice.GreaterThan(max) {
			max = v.FilledPrice
		}
	}
	return max
}

func minPrice(min decimal.Decimal, vs []*gobs.Order) decimal.Decimal {
	for _, v := range vs {
		if v.FilledPrice.LessThan(min) {
			min = v.FilledPrice
		}
	}
	return min
}

func unsoldActions(buys, sells []*gobs.Action) []*gobs.Action {
	var bsize, ssize decimal.Decimal
	for _, s := range sells {
		ssize = ssize.Add(filledSize(s.Orders))
	}
	var unsold []*gobs.Action
	for i, b := range buys {
		if bsize.LessThan(ssize) {
			bsize = bsize.Add(filledSize(b.Orders))
			continue
		}
		unsold = buys[i:]
		break
	}
	return unsold
}

func (v *Looper) Status() *trader.Status {
	actions := v.Actions()
	if len(actions) == 0 {
		return &trader.Status{
			UID:          v.uid,
			ProductID:    v.productID,
			ExchangeName: v.exchangeName,
			Summary: &trader.Summary{
				Budget: v.BudgetAt(0.25),
			},
		}
	}

	nbuys, nsells := 0, 0
	for _, a := range actions {
		if a.Orders[0].Side == "BUY" {
			nbuys++
		} else {
			nsells++
		}
	}

	first := slices.MinFunc(actions, func(a, b *gobs.Action) int {
		return a.Orders[0].CreateTime.Time.Compare(b.Orders[0].CreateTime.Time)
	})

	var pairs [][2][]*gobs.Action

	var pair [2][]*gobs.Action
	for _, a := range actions {
		if a.Orders[0].Side == "BUY" {
			if len(pair[1]) > 0 {
				pairs = append(pairs, pair)
				pair = [2][]*gobs.Action{}
			}
			pair[0] = append(pair[0], a)
		} else {
			pair[1] = append(pair[1], a)
		}
	}
	if len(pair[0]) > 0 {
		pairs = append(pairs, pair)
	}

	var sellFeesTotal, sellSizeTotal, sellValueTotal decimal.Decimal
	var buyFeesTotal, buySizeTotal, buyValueTotal decimal.Decimal
	var unsoldFeesTotal, unsoldSizeTotal, unsoldValueTotal decimal.Decimal
	var oversoldFeesTotal, oversoldSizeTotal, oversoldValueTotal decimal.Decimal

	for _, bs := range pairs {
		var bprice, sprice decimal.Decimal
		var psfees, pssize, psvalue decimal.Decimal
		var pbfees, pbsize, pbvalue decimal.Decimal
		var pufees, pusize, puvalue decimal.Decimal

		for _, s := range bs[1] {
			sfees := filledFee(s.Orders)
			ssize := filledSize(s.Orders)
			svalue := filledValue(s.Orders)

			psfees = psfees.Add(sfees)
			pssize = pssize.Add(ssize)
			psvalue = psvalue.Add(svalue)

			// Find out the worst-case sell price. We use this to approximate
			// oversold items value.
			if sprice.IsZero() {
				sprice = s.Orders[0].FilledPrice
			}
			sprice = minPrice(sprice, s.Orders)
		}

		for _, b := range bs[0] {
			bfees := filledFee(b.Orders)
			bsize := filledSize(b.Orders)
			bvalue := filledValue(b.Orders)

			pbfees = pbfees.Add(bfees)
			pbsize = pbsize.Add(bsize)
			pbvalue = pbvalue.Add(bvalue)

			// Find out the worst-case buy price. We use this to approximate the
			// unsold items value.
			bprice = maxPrice(bprice, b.Orders)
		}

		for _, u := range unsoldActions(bs[0], bs[1]) {
			ufees := filledFee(u.Orders)
			usize := filledSize(u.Orders)
			uvalue := filledValue(u.Orders)

			pufees = pufees.Add(ufees)
			pusize = pusize.Add(usize)
			puvalue = puvalue.Add(uvalue)
		}

		sellFeesTotal = sellFeesTotal.Add(psfees)
		sellSizeTotal = sellSizeTotal.Add(pssize)
		sellValueTotal = sellValueTotal.Add(psvalue)

		buyFeesTotal = buyFeesTotal.Add(pbfees)
		buySizeTotal = buySizeTotal.Add(pbsize)
		buyValueTotal = buyValueTotal.Add(pbvalue)

		unsoldFeesTotal = unsoldFeesTotal.Add(pufees)
		unsoldSizeTotal = unsoldSizeTotal.Add(pusize)
		unsoldValueTotal = unsoldValueTotal.Add(puvalue)

		sizediff := pbsize.Sub(pssize)
		if sizediff.IsNegative() {
			log.Printf("OVERSELL: %s sold size %s, but only bought size %s", bs[0][0].PairingKey, pssize.StringFixed(3), pbsize.StringFixed(3))
			osize := pssize.Sub(pbsize)
			ovalue := sprice.Mul(osize)
			ofees := psfees.Div(pssize).Mul(osize)

			oversoldFeesTotal = oversoldFeesTotal.Add(ofees)
			oversoldSizeTotal = oversoldSizeTotal.Add(osize)
			oversoldValueTotal = oversoldValueTotal.Add(ovalue)
		}

		// Adjustment for fractional unsold parts that are not being handled cause
		// they are too small.
		if sizediff.IsPositive() && !sizediff.Equal(pusize) {
			usize := sizediff.Sub(pusize)
			uvalue := bprice.Mul(usize)
			ufees := pbfees.Div(pbsize).Mul(usize)
			log.Printf("UNDERSELL: %s has unsold size %s that is not covered by any sell actions", bs[0][0].PairingKey, usize.StringFixed(3))

			unsoldFeesTotal = unsoldFeesTotal.Add(ufees)
			unsoldSizeTotal = unsoldSizeTotal.Add(usize)
			unsoldValueTotal = unsoldValueTotal.Add(uvalue)
		}
	}

	s := &trader.Status{
		UID:          v.uid,
		ProductID:    v.productID,
		ExchangeName: v.exchangeName,

		Summary: &trader.Summary{
			NumBuys:  nbuys,
			NumSells: nsells,

			SoldFees:  sellFeesTotal,
			SoldSize:  sellSizeTotal,
			SoldValue: sellValueTotal,

			BoughtFees:  buyFeesTotal,
			BoughtSize:  buySizeTotal,
			BoughtValue: buyValueTotal,

			UnsoldFees:  unsoldFeesTotal,
			UnsoldSize:  unsoldSizeTotal,
			UnsoldValue: unsoldValueTotal,

			OversoldFees:  oversoldFeesTotal,
			OversoldSize:  oversoldSizeTotal,
			OversoldValue: oversoldValueTotal,

			MinCreateTime: first.Orders[0].CreateTime.Time,
		},
	}
	feePct, _ := s.FeePct().Float64()
	s.Budget = v.BudgetAt(feePct)
	return s
}
