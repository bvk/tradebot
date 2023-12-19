// Copyright (c) 2023 BVK Chaitanya

package gobs

type ServerExchangeState struct {
	EnabledProductIDs []string

	WatchedProductIDs []string
}

type ServerState struct {
	ExchangeMap map[string]*ServerExchangeState
}
