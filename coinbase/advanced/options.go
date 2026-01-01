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

	// Max limit for time difference between local time and the server times.
	MaxTimeAdjustment time.Duration

	// Max time latency for fetching the server time from coinbase.
	MaxFetchTimeLatency time.Duration

	// Periodic timeout interval to recalculate time difference between local
	// time and the exchange time.
	SyncTimeInterval time.Duration
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
	if v.MaxTimeAdjustment == 0 {
		v.MaxTimeAdjustment = time.Minute
	}
	if v.MaxFetchTimeLatency == 0 {
		v.MaxFetchTimeLatency = 100 * time.Millisecond
	}
	if v.SyncTimeInterval == 0 {
		v.SyncTimeInterval = 30 * time.Minute
	}
}
