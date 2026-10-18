// Copyright (c) 2023 BVK Chaitanya

package gobs

type LooperState struct {
	V2 *LooperStateV2
}

type LooperStateV2 struct {
	Options map[string]string

	ProductID    string
	ExchangeName string
	AccountName  string
	LimiterIDs   []string
	TradePair    Pair

	LifetimeSummary *Summary
}

func (v *LooperState) Upgrade() {
	if len(v.V2.ExchangeName) == 0 {
		v.V2.ExchangeName = "coinbase"
	}
	if len(v.V2.AccountName) == 0 {
		v.V2.AccountName = v.V2.ExchangeName
	}
}
