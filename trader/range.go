// Copyright (c) 2023 BVK Chaitanya

package trader

import "github.com/shopspring/decimal"

type Range struct {
	IncludeLow  bool
	IncludeHigh bool

	Low  decimal.Decimal
	High decimal.Decimal
}

func (r *Range) IsZero() bool {
	return r.Low.IsZero() && r.High.IsZero()
}

func (r *Range) In(value decimal.Decimal) bool {
	if r.IsZero() {
		return false
	}
	if r.IncludeLow && r.IncludeHigh {
		return value.GreaterThanOrEqual(r.Low) && value.LessThanOrEqual(r.High)
	}
	if r.IncludeLow && !r.IncludeHigh {
		return value.GreaterThanOrEqual(r.Low) && value.LessThan(r.High)
	}
	if !r.IncludeLow && r.IncludeHigh {
		return value.GreaterThan(r.Low) && value.LessThanOrEqual(r.High)
	}
	return value.GreaterThan(r.Low) && value.LessThan(r.High)
}

func (r *Range) Out(value decimal.Decimal) bool {
	if r.IsZero() {
		return false
	}
	if r.IncludeLow && r.IncludeHigh {
		return value.LessThan(r.Low) || value.GreaterThan(r.High)
	}
	if r.IncludeLow && !r.IncludeHigh {
		return value.LessThan(r.Low) || value.GreaterThanOrEqual(r.High)
	}
	if !r.IncludeLow && r.IncludeHigh {
		return value.LessThanOrEqual(r.Low) || value.GreaterThan(r.High)
	}
	return value.LessThanOrEqual(r.Low) || value.GreaterThanOrEqual(r.High)
}
