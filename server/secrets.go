// Copyright (c) 2023 BVK Chaitanya

package server

import (
	"encoding/json"
	"os"

	"github.com/bvk/tradebot/setup"
)

type Secrets struct {
	Coinbase *setup.Coinbase `json:"coinbase"`
	CoinEx   *setup.CoinEx   `json:"coinex"`
	Pushover *setup.Pushover `json:"pushover"`
	Telegram *setup.Telegram `json:"telegram"`
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

func (v *Secrets) Check() error {
	if v.Telegram != nil {
		if err := v.Telegram.Check(); err != nil {
			return err
		}
	}
	return nil
}
