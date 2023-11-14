// Copyright (c) 2023 BVK Chaitanya

package gobs

import (
	"github.com/bvk/tradebot/exchange"
	"github.com/bvk/tradebot/point"
)

type LimiterState struct {
	UID               string
	ProductID         string
	Offset            uint64
	Point             point.Point
	OrderMap          map[exchange.OrderID]*exchange.Order
	ClientServerIDMap map[string]string

	V2 *LimiterStateV2
}

type LimiterStateV2 struct {
	ProductID         string
	ClientIDOffset    uint64
	TradePoint        Point
	ClientServerIDMap map[string]string
	ServerIDOrderMap  map[string]*Order
}

func (v *LimiterState) Upgrade() {
	if v.V2 != nil {
		return
	}
	v.V2 = &LimiterStateV2{
		ProductID:      v.ProductID,
		ClientIDOffset: v.Offset,
		TradePoint: Point{
			Price:  v.Point.Price,
			Size:   v.Point.Size,
			Cancel: v.Point.Cancel,
		},
		ClientServerIDMap: v.ClientServerIDMap,
		ServerIDOrderMap:  make(map[string]*Order),
	}
	for kk, vv := range v.OrderMap {
		order := &Order{
			ServerOrderID: string(vv.OrderID),
			ClientOrderID: string(vv.ClientOrderID),
			CreateTime:    RemoteTime{Time: vv.CreateTime.Time},
			FilledFee:     vv.Fee,
			FilledSize:    vv.FilledSize,
			FilledPrice:   vv.FilledPrice,
			Side:          vv.Side,
			Status:        vv.Status,
			Done:          vv.Done,
			DoneReason:    vv.DoneReason,
		}
		v.V2.ServerIDOrderMap[string(kk)] = order
	}
}
