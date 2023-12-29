// Copyright (c) 2023 BVK Chaitanya

package trader

import (
	"fmt"
	"log"
	"slices"

	"github.com/bvk/tradebot/gobs"
	"github.com/shopspring/decimal"
)

var (
	d100 = decimal.NewFromInt(100)
)

type Status struct {
	Summary

	uid          string
	ProductID    string
	exchangeName string
}

func (s *Status) UID() string {
	return s.uid
}

func (s *Status) String() string {
	return fmt.Sprintf("uid %s product %s bvalue %s s %s usize %s", s.uid, s.ProductID, s.BoughtSize, s.SoldSize, s.UnsoldSize)
}

func GetStatus(job Trader) *Status {
	actions := job.Actions()
	if len(actions) == 0 {
		return nil
	}

	// Verify that order ids are all unique.
	orderIDCounts := make(map[string]int)
	for _, a := range actions {
		for _, v := range a.Orders {
			orderIDCounts[v.ServerOrderID] = orderIDCounts[v.ServerOrderID] + 1
		}
	}
	for id, cnt := range orderIDCounts {
		if cnt > 1 {
			log.Printf("warning: order id %s is found duplicated %d times", id, cnt)
		}
	}

	first := slices.MinFunc(actions, func(a, b *gobs.Action) int {
		return a.Orders[0].CreateTime.Time.Compare(b.Orders[0].CreateTime.Time)
	})

	nbuys, nsells := 0, 0
	for _, a := range actions {
		if a.Orders[0].Side == "BUY" {
			nbuys++
		} else {
			nsells++
		}
	}

	side := func(a *gobs.Action) string {
		return a.Orders[0].Side
	}

	pairedActions := make(map[string][]*gobs.Action)
	for _, a := range actions {
		if len(a.PairingKey) > 0 {
			vs := pairedActions[a.PairingKey]
			pairedActions[a.PairingKey] = append(vs, a)
			continue
		}
	}

	if len(pairedActions) == 0 {
		return nil
	}

	var pairs [][2][]*gobs.Action
	for _, actions := range pairedActions {
		var pair [2][]*gobs.Action
		for _, a := range actions {
			if side(a) == "BUY" {
				pair[0] = append(pair[0], a)
			} else {
				pair[1] = append(pair[1], a)
			}
		}
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

	s := &Status{
		uid:          job.UID(),
		ProductID:    job.ProductID(),
		exchangeName: job.ExchangeName(),

		Summary: Summary{
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
	s.Budget = job.BudgetAt(feePct)
	return s
}
