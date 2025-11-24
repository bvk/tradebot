// Copyright (c) 2025 BVK Chaitanya

package gobs

import (
	"time"

	"github.com/shopspring/decimal"
)

type Summary struct {
	BeginAt time.Time
	EndAt   time.Time

	Exchange  string
	ProductID string

	// Budget holds the max investment required with 0% fees.
	Budget decimal.Decimal

	// NumSells and NumBuys contain number of *units* of buys and sells performed
	// by a job.
	NumSells decimal.Decimal
	NumBuys  decimal.Decimal

	// Bought* fields contain all the buy amounts (including unsolds)
	BoughtFees  decimal.Decimal
	BoughtSize  decimal.Decimal
	BoughtValue decimal.Decimal

	// Sold* fields contain all the sell amounts (including oversolds)
	SoldFees  decimal.Decimal
	SoldSize  decimal.Decimal
	SoldValue decimal.Decimal

	// Unsold* fields contain buy amounts that are not matched with sells.
	UnsoldFees  decimal.Decimal
	UnsoldSize  decimal.Decimal
	UnsoldValue decimal.Decimal

	// Oversold* fields contain sell amounts that are not matched with buys.
	OversoldFees  decimal.Decimal
	OversoldSize  decimal.Decimal
	OversoldValue decimal.Decimal
}

func (s *Summary) IsZero() bool {
	return s.NumSells.IsZero() && s.NumBuys.IsZero() && s.BeginAt.IsZero() && s.EndAt.IsZero()
}

func (s *Summary) Add(v *Summary) {
	if s.BeginAt.IsZero() {
		s.BeginAt = v.BeginAt
	} else if !v.BeginAt.IsZero() && v.BeginAt.Before(s.BeginAt) {
		s.BeginAt = v.BeginAt
	}

	if s.EndAt.IsZero() {
		s.EndAt = v.EndAt
	} else if !v.EndAt.IsZero() && v.EndAt.After(s.EndAt) {
		s.EndAt = v.EndAt
	}

	s.Budget = s.Budget.Add(v.Budget)

	s.NumSells = s.NumSells.Add(v.NumSells)
	s.NumBuys = s.NumBuys.Add(v.NumBuys)

	s.SoldFees = s.SoldFees.Add(v.SoldFees)
	s.SoldSize = s.SoldSize.Add(v.SoldSize)
	s.SoldValue = s.SoldValue.Add(v.SoldValue)

	s.BoughtFees = s.BoughtFees.Add(v.BoughtFees)
	s.BoughtSize = s.BoughtSize.Add(v.BoughtSize)
	s.BoughtValue = s.BoughtValue.Add(v.BoughtValue)

	s.UnsoldFees = s.UnsoldFees.Add(v.UnsoldFees)
	s.UnsoldSize = s.UnsoldSize.Add(v.UnsoldSize)
	s.UnsoldValue = s.UnsoldValue.Add(v.UnsoldValue)

	s.OversoldFees = s.OversoldFees.Add(v.OversoldFees)
	s.OversoldSize = s.OversoldSize.Add(v.OversoldSize)
	s.OversoldValue = s.OversoldValue.Add(v.OversoldValue)
}

var d1 = decimal.NewFromInt(1)
var d100 = decimal.NewFromInt(100)
var d365 = decimal.NewFromInt(365)

func (s *Summary) FeePct() decimal.Decimal {
	value := s.SoldValue.Add(s.BoughtValue)
	if value.IsZero() {
		return decimal.Zero
	}
	return s.Fees().Div(value).Mul(d100)
}

func (s *Summary) Fees() decimal.Decimal {
	return s.SoldFees.Add(s.BoughtFees)
}

func (s *Summary) Profit() decimal.Decimal {
	svol, sfee := s.SoldValue.Sub(s.OversoldValue), s.SoldFees.Sub(s.OversoldFees)
	bvol, bfee := s.BoughtValue.Sub(s.UnsoldValue), s.BoughtFees.Sub(s.UnsoldFees)
	return svol.Sub(bvol).Sub(sfee.Add(bfee))
}

func (s *Summary) Duration() time.Duration {
	if s.IsZero() {
		return 0
	}
	return s.EndAt.Sub(s.BeginAt)
}

func (s *Summary) NumDays() decimal.Decimal {
	if v := decimal.NewFromFloat(s.Duration().Hours() / 24); !v.IsZero() {
		return v
	}
	return decimal.Zero
}

func (s *Summary) ProfitPerDay() decimal.Decimal {
	p := s.Profit()
	if n := s.NumDays(); !n.IsZero() {
		return p.Div(n)
	}
	return p
}

func (s *Summary) ReturnPct() decimal.Decimal {
	if s.Budget.IsZero() {
		return decimal.Zero
	}
	return s.Profit().Div(s.Budget).Mul(d100)
}

func (s *Summary) AnnualPct() decimal.Decimal {
	if s.Budget.IsZero() {
		return decimal.Zero
	}
	return s.ProfitPerDay().Mul(d365).Div(s.Budget).Mul(d100)
}
