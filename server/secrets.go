// Copyright (c) 2023 BVK Chaitanya

package server

import (
	"encoding/json"
	"os"

	"github.com/bvk/tradebot/coinbase"
	"github.com/bvk/tradebot/coinex"
	"github.com/bvk/tradebot/pushover"
)

type Secrets struct {
	Coinbase *coinbase.Credentials `json:"coinbase"`
	CoinEx   *coinex.Credentials   `json:"coinex"`
	Pushover *pushover.Keys        `json:"pushover"`
}

func SecretsFromFile(fpath string) (*Secrets, error) {
	data, err := os.ReadFile(fpath)
	if err != nil {
		return nil, err
	}
	s := new(Secrets)
	if err := json.Unmarshal(data, s); err != nil {
		return nil, err
	}
	return s, nil
}
