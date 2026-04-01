// Copyright (c) 2026 BVK Chaitanya

package api

import (
	"github.com/bvk/tradebot/setup"
)

const SetupPath = "/trader/setup"

// SetupRequest queries and updates the current server setup.
//
// A nil SetupRequest member indicates a query operation for the respective
// item; a non-nil member indicates an update operation for the respective
// item. When updating an item, response contains the overwritten value.
//
// Note that if tradebot service has already started, updating the setup items
// will not automatically take effect until the service is restarted. Thus, a
// second setup request returns configured values, but not necessarily the
// in-use values.
type SetupRequest struct {
	Coinbase *setup.Coinbase `json:"coinbase"`
	CoinEx   *setup.CoinEx   `json:"coinex"`
	Pushover *setup.Pushover `json:"pushover"`
	Telegram *setup.Telegram `json:"telegram"`
}

type SetupResponse struct {
	Coinbase *setup.Coinbase `json:"coinbase"`
	CoinEx   *setup.CoinEx   `json:"coinex"`
	Pushover *setup.Pushover `json:"pushover"`
	Telegram *setup.Telegram `json:"telegram"`
}

func (v *SetupRequest) Check() error {
	return nil
}
