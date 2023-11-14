// Copyright (c) 2023 BVK Chaitanya

package gobs

import "github.com/bvk/tradebot/point"

type LooperState struct {
	ProductID string
	Limiters  []string
	BuyPoint  point.Point
	SellPoint point.Point

	V2 *LooperStateV2
}

type LooperStateV2 struct {
	ProductID  string
	LimiterIDs []string
	TradePair  Pair
}

func (v *LooperState) Upgrade() {
	if v.V2 != nil {
		return
	}
	v.V2 = &LooperStateV2{
		ProductID:  v.ProductID,
		LimiterIDs: v.Limiters,
		TradePair: Pair{
			Buy: Point{
				Price:  v.BuyPoint.Price,
				Size:   v.BuyPoint.Size,
				Cancel: v.BuyPoint.Cancel,
			},
			Sell: Point{
				Price:  v.SellPoint.Price,
				Size:   v.SellPoint.Size,
				Cancel: v.SellPoint.Cancel,
			},
		},
	}
}
