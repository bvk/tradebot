// Copyright (c) 2025 BVK Chaitanya

package internal

import (
	"net/url"
	"time"
)

var (
	RestURL = url.URL{
		Scheme: "https",
		Host:   "api.coinex.com",
		Path:   "/v2",
	}

	WebsocketURL = url.URL{
		Scheme: "wss",
		Host:   "socket.coinex.com",
		Path:   "/v2/spot",
	}
)

type Options struct {
	// Timeout to use for the HTTP requests.
	HttpClientTimeout time.Duration
}

func (v *Options) setDefaults() {
	if v.HttpClientTimeout == 0 {
		v.HttpClientTimeout = 5 * time.Second
	}
}

// Check validates the options.
func (v *Options) Check() error {
	return nil
}
