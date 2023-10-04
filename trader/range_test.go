// Copyright (c) 2018 BVK Chaitanya

package trader

import (
	"testing"

	"github.com/shopspring/decimal"
)

func inFloat(r *Range, value float64) bool {
	return r.In(decimal.NewFromFloat(value))
}

func outFloat(r *Range, value float64) bool {
	return r.Out(decimal.NewFromFloat(value))
}

func TestRange(test *testing.T) {
	r := &Range{
		Low:  decimal.NewFromFloat(100),
		High: decimal.NewFromFloat(200),
	}
	if inFloat(r, 10) {
		test.Errorf("10 must not be inside (100,200) range")
	}
	if !outFloat(r, 10) {
		test.Errorf("10 must be outside (100,200) range")
	}
	if inFloat(r, 210) {
		test.Errorf("210 must not be inside (100,200) range")
	}
	if !outFloat(r, 210) {
		test.Errorf("210 must be outside (100,200) range")
	}

	if !inFloat(r, 150) {
		test.Errorf("150 must be inside (100,200) range")
	}
	if outFloat(r, 150) {
		test.Errorf("150 must not be outside (100,200) range")
	}

	if inFloat(r, 100) {
		test.Errorf("100 must not be inside (100,200) range")
	}
	if !outFloat(r, 100) {
		test.Errorf("100 must be outside (100,200) range")
	}
	if inFloat(r, 200) {
		test.Errorf("200 must not be inside (100,200) range")
	}
	if !outFloat(r, 200) {
		test.Errorf("200 must be outside (100,200) range")
	}

	r.IncludeLow = true
	r.IncludeHigh = true
	if !inFloat(r, 100) {
		test.Errorf("100 must be inside [100,200] range")
	}
	if outFloat(r, 100) {
		test.Errorf("100 must not be outside [100,200] range")
	}

	if !inFloat(r, 200) {
		test.Errorf("200 must be inside [100,200] range")
	}
	if outFloat(r, 200) {
		test.Errorf("200 must not be outside [100,200] range")
	}

	r.IncludeLow = false
	if inFloat(r, 100) {
		test.Errorf("100 must not be inside (100,200] range")
	}
	if !outFloat(r, 100) {
		test.Errorf("100 must be outside (100,200] range")
	}
	if !inFloat(r, 200) {
		test.Errorf("200 must be inside (100,200] range")
	}
	if outFloat(r, 200) {
		test.Errorf("200 must not be outside (100,200] range")
	}
}
