// Copyright (c) 2023 BVK Chaitanya

package gobs

import "github.com/bvk/tradebot/point"

type WallerState struct {
	ProductID  string
	BuyPoints  []*point.Point
	SellPoints []*point.Point
	Loopers    []string

	V2 *WallerStateV2
}

type WallerStateV2 struct {
	ProductID  string
	LooperIDs  []string
	TradePairs []*Pair
}

func (v *WallerState) Upgrade() {
	if v.V2 != nil {
		return
	}
	v.V2 = &WallerStateV2{
		ProductID:  v.ProductID,
		LooperIDs:  v.Loopers,
		TradePairs: make([]*Pair, len(v.BuyPoints)),
	}
	for i := range v.BuyPoints {
		v.V2.TradePairs[i] = &Pair{
			Buy: Point{
				Price:  v.BuyPoints[i].Price,
				Size:   v.BuyPoints[i].Size,
				Cancel: v.BuyPoints[i].Cancel,
			},
			Sell: Point{
				Price:  v.SellPoints[i].Price,
				Size:   v.SellPoints[i].Size,
				Cancel: v.SellPoints[i].Cancel,
			},
		}
	}
}
