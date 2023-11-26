// Copyright (c) 2023 BVK Chaitanya

package gobs

type ServerJobState struct {
	JobName string

	CurrentState string

	NeedsManualResume bool
}

type NameData struct {
	Name string

	Data string
}

type ServerExchangeState struct {
	EnabledProductIDs []string

	WatchedProductIDs []string
}

type ServerState struct {
	ExchangeMap map[string]*ServerExchangeState
}
