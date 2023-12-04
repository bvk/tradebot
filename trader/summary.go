// Copyright (c) 2023 BVK Chaitanya

package trader

import (
	"time"

	"github.com/shopspring/decimal"
)

type Summary struct {
	NumDays int

	SoldValue   decimal.Decimal
	BoughtValue decimal.Decimal
	UnsoldValue decimal.Decimal

	SoldSize   decimal.Decimal
	BoughtSize decimal.Decimal
	UnsoldSize decimal.Decimal

	TotalFees  decimal.Decimal
	UnsoldFees decimal.Decimal
}

func (s *Summary) FeePct() decimal.Decimal {
	return s.TotalFees.Mul(d100).Div(s.SoldValue.Add(s.BoughtValue))
}

func (s *Summary) Profit() decimal.Decimal {
	unsold := s.UnsoldValue.Add(s.UnsoldFees)
	return s.SoldValue.Sub(s.BoughtValue).Sub(s.TotalFees).Add(unsold)
}

func (s *Summary) ProfitPerDay() decimal.Decimal {
	return s.Profit().Div(decimal.NewFromInt(int64(s.NumDays)))
}

func Summarize(statuses []*Status) *Summary {
	sum := new(Summary)

	var startTime time.Time
	for _, s := range statuses {
		sum.SoldValue = sum.SoldValue.Add(s.soldValue)
		sum.BoughtValue = sum.BoughtValue.Add(s.boughtValue)
		sum.UnsoldValue = sum.UnsoldValue.Add(s.unsoldValue)

		sum.SoldSize = sum.SoldSize.Add(s.soldSize)
		sum.BoughtSize = sum.BoughtSize.Add(s.boughtSize)
		sum.UnsoldSize = sum.UnsoldSize.Add(s.unsoldSize)

		sum.TotalFees = sum.TotalFees.Add(s.totalFees)
		sum.UnsoldFees = sum.UnsoldFees.Add(s.unsoldFees)

		if startTime.IsZero() || s.StartTime().Before(startTime) {
			startTime = s.StartTime()
		}
	}

	sum.NumDays = int(time.Now().Sub(startTime) / (24 * time.Hour))
	return sum
}
