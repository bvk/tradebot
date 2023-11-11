// Copyright (c) 2023 BVK Chaitanya

package point

import (
	"testing"

	"github.com/shopspring/decimal"
)

func TestAdjustForMargin(t *testing.T) {
	p := &Pair{
		Buy: Point{
			Size:   decimal.NewFromInt(1),
			Price:  decimal.NewFromInt(100),
			Cancel: decimal.NewFromInt(100 + 10),
		},
		Sell: Point{
			Size:   decimal.NewFromInt(1),
			Price:  decimal.NewFromInt(200),
			Cancel: decimal.NewFromInt(200 - 10),
		},
	}

	np := AdjustForMargin(p, 0.25)
	margin := np.Margin().Sub(np.FeesAt(0.25))

	if v := p.Margin(); !v.Equal(margin) {
		t.Fatalf("want %s == %s", v, margin)
	}
}
