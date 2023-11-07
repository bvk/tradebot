// Copyright (c) 2023 BVK Chaitanya

package trader

import (
	"encoding/json"
	"os"

	"github.com/bvk/tradebot/coinbase"
)

type Secrets struct {
	Coinbase *coinbase.Credentials
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
