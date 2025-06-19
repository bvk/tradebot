// Copyright (c) 2024 BVK Chaitanya

package looper

import (
	"log"
	"slices"
	"time"

	"github.com/bvk/tradebot/exchange"
	"github.com/bvk/tradebot/gobs"
	"github.com/bvk/tradebot/timerange"
	"github.com/bvk/tradebot/trader"
	"github.com/shopspring/decimal"
)

func unsoldActions(buys, sells []*gobs.Action) []*gobs.Action {
	var bsize, ssize decimal.Decimal
	for _, s := range sells {
		ssize = ssize.Add(exchange.FilledSize(s.Orders))
	}
	var unsold []*gobs.Action
	for i, b := range buys {
		if bsize.LessThan(ssize) {
			bsize = bsize.Add(exchange.FilledSize(b.Orders))
			continue
		}
		unsold = buys[i:]
		break
	}
	return unsold
}

func actionTime(actions []*gobs.Action) time.Time {
	var max time.Time
	for i, a := range actions {
		lastOrder := slices.MaxFunc(a.Orders, func(i, j *gobs.Order) int {
			return i.FinishTime.Time.Compare(j.FinishTime.Time)
		})
		if i == 0 || max.Before(lastOrder.FinishTime.Time) {
			max = lastOrder.FinishTime.Time
		}
	}
	if len(actions) > 0 && max.IsZero() {
		log.Printf("finish Time field is empty for one or more actions for %s; using create time instead", actions[0].UID)
		for i, a := range actions {
			lastOrder := slices.MaxFunc(a.Orders, func(i, j *gobs.Order) int {
				return i.CreateTime.Time.Compare(j.CreateTime.Time)
			})
			if i == 0 || max.Before(lastOrder.CreateTime.Time) {
				max = lastOrder.CreateTime.Time
			}
		}
	}
	return max
}

func (v *Looper) Status(period *timerange.Range) *trader.Status {
	actions := v.Actions()
	if len(actions) == 0 {
		return &trader.Status{
			UID:          v.uid,
			ProductID:    v.productID,
			ExchangeName: v.exchangeName,
			Summary: &trader.Summary{
				Budget: v.BudgetAt(decimal.NewFromFloat(0.25)),
			},
		}
	}

	first := slices.MinFunc(actions, func(a, b *gobs.Action) int {
		return a.Orders[0].CreateTime.Time.Compare(b.Orders[0].CreateTime.Time)
	})
	if period == nil || period.IsZero() {
		period = &timerange.Range{Begin: first.Orders[0].CreateTime.Time}
	}

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

	nbuys, nsells := 0, 0
	var sellFeesTotal, sellSizeTotal, sellValueTotal decimal.Decimal
	var buyFeesTotal, buySizeTotal, buyValueTotal decimal.Decimal
	var unsoldFeesTotal, unsoldSizeTotal, unsoldValueTotal decimal.Decimal
	var oversoldFeesTotal, oversoldSizeTotal, oversoldValueTotal decimal.Decimal

	for _, bs := range pairs {
		btime := actionTime(bs[0])
		stime := actionTime(bs[1])
		if !period.InRange(btime) && !period.InRange(stime) {
			continue
		}

		buyInRange := period.InRange(actionTime(bs[0]))
		sellInRange := period.InRange(actionTime(bs[1]))

		var bprice, sprice decimal.Decimal
		var psfees, pssize, psvalue decimal.Decimal
		var pbfees, pbsize, pbvalue decimal.Decimal
		var pufees, pusize, puvalue decimal.Decimal

		if sellInRange {
			nsells++
			for _, s := range bs[1] {
				sfees := exchange.FilledFee(s.Orders)
				ssize := exchange.FilledSize(s.Orders)
				svalue := exchange.FilledValue(s.Orders)

				psfees = psfees.Add(sfees)
				pssize = pssize.Add(ssize)
				psvalue = psvalue.Add(svalue)

				// Find out the worst-case sell price. We use this to approximate
				// oversold items value.
				if sprice.IsZero() {
					sprice = s.Orders[0].FilledPrice
				}
				sprice = decimal.Min(sprice, exchange.MinPrice(s.Orders))
			}
		}

		if buyInRange {
			nbuys++
		}

		if buyInRange || sellInRange {
			for _, b := range bs[0] {
				bfees := exchange.FilledFee(b.Orders)
				bsize := exchange.FilledSize(b.Orders)
				bvalue := exchange.FilledValue(b.Orders)

				pbfees = pbfees.Add(bfees)
				pbsize = pbsize.Add(bsize)
				pbvalue = pbvalue.Add(bvalue)

				// Find out the worst-case buy price. We use this to approximate the
				// unsold items value.
				bprice = decimal.Max(bprice, exchange.MaxPrice(b.Orders))
			}
		}

		for _, u := range unsoldActions(bs[0], bs[1]) {
			ufees := exchange.FilledFee(u.Orders)
			usize := exchange.FilledSize(u.Orders)
			uvalue := exchange.FilledValue(u.Orders)

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
			// log.Printf("OVERSELL: %s sold size %s, but only bought size %s", bs[0][0].PairingKey, pssize.StringFixed(3), pbsize.StringFixed(3))
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
			// log.Printf("UNDERSELL: %s has unsold size %s that is not covered by any sell actions", bs[0][0].PairingKey, usize.StringFixed(3))

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

			TimePeriod: *period,
		},
	}
	s.Budget = v.BudgetAt(s.FeePct())
	return s
}
