// Copyright (c) 2023 BVK Chaitanya

package gobs

type LooperState struct {
	V2 *LooperStateV2
}

type LooperStateV2 struct {
	ProductID    string
	ExchangeName string
	LimiterIDs   []string
	TradePair    Pair
}

func (v *LooperState) Upgrade() {
	if len(v.V2.ExchangeName) == 0 {
		v.V2.ExchangeName = "coinbase"
	}
}
