// Copyright (c) 2023 BVK Chaitanya

package trader

import (
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

	soldSize   decimal.Decimal
	boughtSize decimal.Decimal
	unsoldSize decimal.Decimal

	soldValue   decimal.Decimal
	boughtValue decimal.Decimal
	unsoldValue decimal.Decimal

	totalFees  decimal.Decimal
	unsoldFees decimal.Decimal
}

func (s *Status) UID() string {
	return s.uid
}

func (s *Status) ProductID() string {
	return s.productID
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
	return s.soldValue.Sub(s.boughtValue).Sub(s.totalFees).Add(unsold)
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

	var fees decimal.Decimal
	var bsize, ssize decimal.Decimal
	var bvalue, svalue decimal.Decimal
	for _, bs := range pairs {
		fees = fees.Add(filledFee(bs[0].Orders))
		bsize = bsize.Add(filledSize(bs[0].Orders))
		bvalue = bvalue.Add(filledValue(bs[0].Orders))

		fees = fees.Add(filledFee(bs[1].Orders))
		ssize = ssize.Add(filledSize(bs[1].Orders))
		svalue = svalue.Add(filledValue(bs[1].Orders))
	}

	var ufees decimal.Decimal
	var usize, uvalue decimal.Decimal
	for i, v := range unpaired {
		if side(v) != "BUY" {
			log.Printf("%s: unpaired order %d (%#v) is not a buy order (ignored)", job.UID(), i, v)
			continue
		}
		fees = fees.Add(filledFee(v.Orders))
		bsize = bsize.Add(filledSize(v.Orders))
		bvalue = bvalue.Add(filledValue(v.Orders))

		ufees = ufees.Add(filledFee(v.Orders))
		usize = usize.Add(filledSize(v.Orders))
		uvalue = uvalue.Add(filledValue(v.Orders))
	}

	s := &Status{
		uid:          job.UID(),
		productID:    job.ProductID(),
		exchangeName: job.ExchangeName(),

		soldValue:   svalue,
		boughtValue: bvalue,
		unsoldValue: uvalue,

		soldSize:   ssize,
		boughtSize: bsize,
		unsoldSize: usize,

		totalFees:  fees,
		unsoldFees: ufees,

		minCreateTime: first.Orders[0].CreateTime.Time,
	}
	return s
}
