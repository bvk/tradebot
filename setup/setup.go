// Copyright (c) 2026 BVK Chaitanya

package setup

type Coinbase struct {
	KID string `json:"kid"`
	PEM string `json:"pem"`
}

type CoinEx struct {
	Key    string `json:"key"`
	Secret string `json:"secret"`
}

type Pushover struct {
	ApplicationKey string `json:"application_key"`
	UserKey        string `json:"user_key"`
}
