// Copyright (c) 2023 BVK Chaitanya

package gobs

type WallerState struct {
	V2 *WallerStateV2
}

type WallerStateV2 struct {
	ProductID    string
	ExchangeName string
	AccountName  string

	LooperIDs  []string
	TradePairs []*Pair

	LifetimeSummary *Summary
}

func (v *WallerState) Upgrade() {
	if len(v.V2.ExchangeName) == 0 {
		v.V2.ExchangeName = "coinbase"
	}
	if len(v.V2.AccountName) == 0 {
		v.V2.AccountName = v.V2.ExchangeName
	}
}
