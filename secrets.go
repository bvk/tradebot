// Copyright (c) 2023 BVK Chaitanya

package main

import (
	"encoding/json"
	"io/ioutil"

	"github.com/bvkgo/tradebot/coinbase"
)

type secrets struct {
	Coinbase coinbase.Credentials
}

func secretsFromFile(fpath string) (*secrets, error) {
	data, err := ioutil.ReadFile(fpath)
	if err != nil {
		return nil, err
	}
	s := new(secrets)
	if err := json.Unmarshal(data, s); err != nil {
		return nil, err
	}
	return s, nil
}
