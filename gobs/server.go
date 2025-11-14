// Copyright (c) 2023 BVK Chaitanya

package gobs

import "github.com/shopspring/decimal"

type ServerExchangeState struct {
	EnabledProductIDs []string

	WatchedProductIDs []string
}

type AlertsConfig struct {
	PerExchangeConfig map[string]*AlertsConfig

	LowBalanceLimits map[string]decimal.Decimal
}

type ServerState struct {
	AlertsConfig *AlertsConfig

	ExchangeMap map[string]*ServerExchangeState
}
