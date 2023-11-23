// Copyright (c) 2023 BVK Chaitanya

package gobs

type TraderJobState struct {
	JobName string

	CurrentState string

	NeedsManualResume bool
}

type NameData struct {
	Name string

	Data string
}

type TraderExchangeState struct {
	EnabledProductIDs []string

	WatchedProductIDs []string
}

type TraderState struct {
	ExchangeMap map[string]*TraderExchangeState
}
