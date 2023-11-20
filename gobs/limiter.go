// Copyright (c) 2023 BVK Chaitanya

package gobs

type LimiterState struct {
	V2 *LimiterStateV2
}

type LimiterStateV2 struct {
	ProductID         string
	ClientIDSeed      string
	ClientIDOffset    uint64
	TradePoint        Point
	ClientServerIDMap map[string]string
	ServerIDOrderMap  map[string]*Order
}

func (v *LimiterState) Upgrade() {
}
