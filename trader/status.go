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

// func (s *Status) NumDays() int {
// 	return int(time.Now().Sub(s.MinCreateTime) / (24 * time.Hour))
// }

// func (s *Status) ARR() decimal.Decimal {
// 	perDay := s.Profit().Div(decimal.NewFromInt(int64(s.NumDays())))
// 	perYear := perDay.Mul(decimal.NewFromInt(365))
// 	return perYear.Mul(decimal.NewFromInt(100)).Div(s.Budget)
// }

// func (s *Status) Sold() decimal.Decimal {
// 	return s.SoldValue.Sub(s.OversoldValue)
// }

// func (s *Status) Bought() decimal.Decimal {
// 	return s.BoughtValue.Sub(s.UnsoldValue)
// }

// func (s *Status) Fees() decimal.Decimal {
// 	sfees := s.SoldFees.Sub(s.OversoldFees)
// 	bfees := s.BoughtFees.Sub(s.UnsoldFees)
// 	return sfees.Add(bfees)
// }

// func (s *Status) Profit() decimal.Decimal {
// 	svalue := s.SoldValue.Sub(s.OversoldValue)
// 	bvalue := s.BoughtValue.Sub(s.UnsoldValue)
// 	sfees := s.SoldFees.Sub(s.OversoldFees)
// 	bfees := s.BoughtFees.Sub(s.UnsoldFees)
// 	profit := svalue.Sub(bvalue).Sub(bfees).Sub(sfees)
// 	return profit
// }

// func (s *Status) FeePct() decimal.Decimal {
// 	divisor := s.SoldValue.Add(s.BoughtValue)
// 	if divisor.IsZero() {
// 		return decimal.Zero
// 	}
// 	totalFees := s.SoldFees.Add(s.BoughtFees)
// 	return totalFees.Mul(d100).Div(divisor)
// }

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

		if pssize.GreaterThan(pbsize) {
			log.Printf("OVERSELL: %s sold size %s, but only bought size %s", bs[0][0].PairingKey, pssize.StringFixed(3), pbsize.StringFixed(3))
			osize := pssize.Sub(pbsize)
			ovalue := sprice.Mul(osize)
			ofees := psfees.Div(pssize).Mul(osize)

			oversoldFeesTotal = oversoldFeesTotal.Add(ofees)
			oversoldSizeTotal = oversoldSizeTotal.Add(osize)
			oversoldValueTotal = oversoldValueTotal.Add(ovalue)
		}

		sellFeesTotal = sellFeesTotal.Add(psfees)
		sellSizeTotal = sellSizeTotal.Add(pssize)
		sellValueTotal = sellValueTotal.Add(psvalue)

		buyFeesTotal = buyFeesTotal.Add(pbfees)
		buySizeTotal = buySizeTotal.Add(pbsize)
		buyValueTotal = buyValueTotal.Add(pbvalue)

		if pssize.LessThan(pbsize) {
			usize := pbsize.Sub(pssize)
			uvalue := bprice.Mul(usize)
			ufees := pbfees.Div(pbsize).Mul(usize)

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
