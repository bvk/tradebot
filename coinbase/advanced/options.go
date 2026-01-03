// Copyright (c) 2023 BVK Chaitanya

package advanced

import "time"

var (
	RestHostname      = "api.coinbase.com"
	WebsocketHostname = "advanced-trade-ws.coinbase.com"
)

type Options struct {
	// Hostnames for the REST and WebSocket service endpoints.
	RestHostname      string
	WebsocketHostname string

	// Timeout to use for the HTTP requests.
	HttpClientTimeout time.Duration

	// Timeout interval to create a new websocket session after a failure.
	WebsocketRetryInterval time.Duration
}

func (v *Options) setDefaults() {
	if v.RestHostname == "" {
		v.RestHostname = RestHostname
	}
	if v.WebsocketHostname == "" {
		v.WebsocketHostname = WebsocketHostname
	}
	if v.HttpClientTimeout == 0 {
		v.HttpClientTimeout = 5 * time.Second
	}
	if v.WebsocketRetryInterval == 0 {
		v.WebsocketRetryInterval = time.Second
	}
}
