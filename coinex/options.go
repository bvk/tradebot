// Copyright (c) 2025 BVK Chaitanya

package coinex

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

	// Max limit for time difference between local time and the server times.
	MaxTimeAdjustment time.Duration

	// Max time latency for fetching the server time from coinbase.
	MaxFetchTimeLatency time.Duration

	// WebsocketPingInterval holds ping-pong interval for the websockets.
	WebsocketPingInterval time.Duration

	// RefreshOrdersInterval holds query orders interval to asynchronously query
	// order statuses to handle websocket failure scenario.
	RefreshOrdersInterval time.Duration

	BatchQueryOrdersSize int
}

func (v *Options) setDefaults() {
	if v.HttpClientTimeout == 0 {
		v.HttpClientTimeout = 5 * time.Second
	}
	if v.MaxTimeAdjustment == 0 {
		v.MaxTimeAdjustment = 5 * time.Minute
	}
	if v.MaxFetchTimeLatency == 0 {
		v.MaxFetchTimeLatency = 500 * time.Millisecond
	}
	if v.WebsocketPingInterval == 0 {
		v.WebsocketPingInterval = 30 * time.Second
	}
	if v.RefreshOrdersInterval == 0 {
		v.RefreshOrdersInterval = 30 * time.Second
	}
	if v.BatchQueryOrdersSize == 0 {
		v.BatchQueryOrdersSize = 25
	}
}

// Check validates the options.
func (v *Options) Check() error {
	return nil
}
