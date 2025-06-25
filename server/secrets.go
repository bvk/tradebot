// Copyright (c) 2023 BVK Chaitanya

package server

import (
	"encoding/json"
	"os"

	"github.com/bvk/tradebot/coinbase"
	"github.com/bvk/tradebot/coinex"
	"github.com/bvk/tradebot/pushover"
	"github.com/bvk/tradebot/telegram"
)

type Secrets struct {
	Coinbase *coinbase.Credentials `json:"coinbase"`
	CoinEx   *coinex.Credentials   `json:"coinex"`
	Pushover *pushover.Keys        `json:"pushover"`
	Telegram *telegram.Secrets     `json:"telegram"`
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
