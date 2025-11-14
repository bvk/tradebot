// Copyright (c) 2023 BVK Chaitanya

package gobs

import "github.com/shopspring/decimal"

type ServerExchangeState struct {
	EnabledProductIDs []string

	WatchedProductIDs []string
}

type AlertsConfig struct {
	LowBalanceLimits map[string]decimal.Decimal
}

type ServerState struct {
	AlertsConfig *AlertsConfig

	ExchangeMap map[string]*ServerExchangeState
}
