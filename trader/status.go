// Copyright (c) 2023 BVK Chaitanya

package trader

import (
	"fmt"
	"log"
	"slices"
	"time"

	"github.com/bvk/tradebot/gobs"
	"github.com/bvk/tradebot/point"
	"github.com/shopspring/decimal"
)

var (
	d100 = decimal.NewFromInt(100)
)

type Status struct {
	uid          string
	productID    string
	exchangeName string

	minCreateTime time.Time

	soldSize     decimal.Decimal
	boughtSize   decimal.Decimal
	unsoldSize   decimal.Decimal
	oversoldSize decimal.Decimal

	soldValue     decimal.Decimal
	boughtValue   decimal.Decimal
	unsoldValue   decimal.Decimal
	oversoldValue decimal.Decimal

	totalFees    decimal.Decimal
	unsoldFees   decimal.Decimal
	oversoldFees decimal.Decimal
}

func (s *Status) UID() string {
	return s.uid
}

func (s *Status) ProductID() string {
	return s.productID
}

func (s *Status) String() string {
	return fmt.Sprintf("uid %s product %s bvalue %s s %s usize %s", s.uid, s.productID, s.boughtSize, s.soldSize, s.unsoldSize)
}

func (s *Status) TotalFees() decimal.Decimal {
	return s.totalFees
}

func (s *Status) BoughtValue() decimal.Decimal {
	return s.boughtValue
}

func (s *Status) SoldValue() decimal.Decimal {
	return s.soldValue
}

func (s *Status) UnsoldValue() decimal.Decimal {
	return s.unsoldValue
}

func (s *Status) StartTime() time.Time {
	return s.minCreateTime
}

func (s *Status) Profit() decimal.Decimal {
	unsold := s.unsoldValue.Add(s.unsoldFees)
	oversold := s.oversoldValue.Add(s.oversoldFees)
	profit := s.soldValue.Sub(s.boughtValue).Sub(s.totalFees).Add(unsold)
	return profit.Sub(oversold)
}

func (s *Status) FeePct() decimal.Decimal {
	return s.totalFees.Mul(d100).Div(s.soldValue.Add(s.boughtValue))
}

func GetStatus(job Job) *Status {
	actions := job.Actions()
	if len(actions) == 0 {
		return nil
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

	var pairs [][2]*gobs.Action
	var unpaired []*gobs.Action
	for i := 0; i < len(actions); i++ {
		this := actions[i]
		if i < len(actions)-1 {
			next := actions[i+1]
			if side(this) == "BUY" && side(next) == "SELL" {
				pairs = append(pairs, [2]*gobs.Action{this, next})
				i++
				continue
			}
		}

		// This adjustment is necessary cause we had an incident that required
		// multiple sells for a single buy.
		if side(this) == "SELL" {
			if len(pairs) > 0 {
				last := pairs[len(pairs)-1]
				if point.Equal(last[1].Point, this.Point) {
					last[1].Orders = append(last[1].Orders, this.Orders...)
				}
			} else {
				log.Printf("%s: noticed a sell for point %s before any buy-sell pairs", job.UID(), this.Point)
			}
			continue
		}

		unpaired = append(unpaired, this)
	}

	var feesTotal decimal.Decimal
	var buySizeTotal, buyValueTotal decimal.Decimal
	var sellSizeTotal, sellValueTotal decimal.Decimal
	var unsoldFeesTotal, unsoldSizeTotal, unsoldValueTotal decimal.Decimal
	var oversoldFeesTotal, oversoldSizeTotal, oversoldValueTotal decimal.Decimal
	for _, bs := range pairs {
		bfees := filledFee(bs[0].Orders)
		bsize := filledSize(bs[0].Orders)
		bvalue := filledValue(bs[0].Orders)
		bprice := avgPrice(bs[0].Orders)

		sfees := filledFee(bs[1].Orders)
		ssize := filledSize(bs[1].Orders)
		svalue := filledValue(bs[1].Orders)
		sprice := avgPrice(bs[1].Orders)

		feesTotal = feesTotal.Add(bfees)
		buySizeTotal = buySizeTotal.Add(bsize)
		buyValueTotal = buyValueTotal.Add(bvalue)

		feesTotal = feesTotal.Add(sfees)
		sellSizeTotal = sellSizeTotal.Add(ssize)
		sellValueTotal = sellValueTotal.Add(svalue)

		if ssize.LessThan(bsize) {
			log.Printf("UNDERSELL: %s sold only size %s of buy size %s by %s", bs[1].UID, ssize.StringFixed(3), bsize.StringFixed(3), bs[0].UID)
			usize := bsize.Sub(ssize)
			uvalue := bvalue.Sub(svalue)
			ufees := bfees.Div(bsize).Mul(usize)

			unsoldFeesTotal = unsoldFeesTotal.Add(ufees)
			unsoldSizeTotal = unsoldSizeTotal.Add(usize)
			unsoldValueTotal = unsoldValueTotal.Add(uvalue)
		}
		if ssize.GreaterThan(bsize) {
			log.Printf("OVERSELL: %s sold size %s at %s, but only bought size %s at %s by %s", bs[1].UID, ssize.StringFixed(3), sprice.StringFixed(3), bsize.StringFixed(3), bprice.StringFixed(3), bs[0].UID)
			osize := ssize.Sub(bsize)
			ovalue := svalue.Sub(bvalue)
			ofees := sfees.Div(ssize).Mul(osize)

			oversoldFeesTotal = oversoldFeesTotal.Add(ofees)
			oversoldSizeTotal = oversoldSizeTotal.Add(osize)
			oversoldValueTotal = oversoldValueTotal.Add(ovalue)
		}
		if sprice.LessThan(bprice) {
			log.Printf("LOSS: %s sold size %s at %s of buy size %s at %s by %s", bs[1].UID, ssize.StringFixed(3), sprice.StringFixed(3), bsize.StringFixed(3), bprice.StringFixed(3), bs[0].UID)
			// TODO: Keep accounting for all losses.
		}
	}

	for i, v := range unpaired {
		if side(v) != "BUY" {
			log.Printf("%s: unpaired order %d (%#v) is not a buy order (ignored)", job.UID(), i, v)
			continue
		}

		ufees := filledFee(v.Orders)
		usize := filledSize(v.Orders)
		uvalue := filledValue(v.Orders)

		feesTotal = feesTotal.Add(ufees)
		buySizeTotal = buySizeTotal.Add(usize)
		buyValueTotal = buyValueTotal.Add(uvalue)

		unsoldFeesTotal = unsoldFeesTotal.Add(ufees)
		unsoldSizeTotal = unsoldSizeTotal.Add(usize)
		unsoldValueTotal = unsoldValueTotal.Add(uvalue)
	}

	s := &Status{
		uid:          job.UID(),
		productID:    job.ProductID(),
		exchangeName: job.ExchangeName(),

		soldValue:     sellValueTotal,
		boughtValue:   buyValueTotal,
		unsoldValue:   unsoldValueTotal,
		oversoldValue: oversoldValueTotal,

		soldSize:     sellSizeTotal,
		boughtSize:   buySizeTotal,
		unsoldSize:   unsoldSizeTotal,
		oversoldSize: oversoldSizeTotal,

		totalFees:    feesTotal,
		unsoldFees:   unsoldFeesTotal,
		oversoldFees: oversoldFeesTotal,

		minCreateTime: first.Orders[0].CreateTime.Time,
	}
	return s
}
