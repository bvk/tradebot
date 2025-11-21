// Copyright (c) 2023 BVK Chaitanya

package gobs

type LimiterState struct {
	V2 *LimiterStateV2
}

type LimiterStateV2 struct {
	Options map[string]string

	ProductID        string
	ExchangeName     string
	ClientIDSeed     string
	ClientIDOffset   uint64
	TradePoint       Point
	ServerIDOrderMap map[string]*Order
}

func (v *LimiterState) Upgrade() {
	if len(v.V2.ExchangeName) == 0 {
		v.V2.ExchangeName = "coinbase"
	}
}
