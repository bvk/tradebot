// Copyright (c) 2023 BVK Chaitanya

package gobs

type WallerState struct {
	V2 *WallerStateV2
}

type WallerStateV2 struct {
	ProductID    string
	ExchangeName string
	LooperIDs    []string
	TradePairs   []*Pair
}

func (v *WallerState) Upgrade() {
	if len(v.V2.ExchangeName) == 0 {
		v.V2.ExchangeName = "coinbase"
	}
}
