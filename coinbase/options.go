// Copyright (c) 2023 BVK Chaitanya

package coinbase

import "time"

var (
	RestHostname      = "coinbase.com"
	WebsocketHostname = "advanced-trade-ws.coinbase.com"
)

type Options struct {
	// Hostnames for the REST and WebSocket service endpoints.
	RestHostname      string
	WebsocketHostname string

	// Timeout to use for the HTTP requests.
	HttpClientTimeout time.Duration

	// RetryCount indicates number of times to retry using exponential backoff.
	RetryCount uint

	// Timeout interval to create a new websocket session after a failure.
	WebsocketRetryInterval time.Duration

	// Timeout interval to retry list-orders polling operation.
	PollOrdersRetryInterval time.Duration

	// Max number of out of order websocket messages allowed before restarting
	// the websocket.
	MaxWebsocketOutOfOrderAllowance int

	// Max limit for time difference between local time and the server times.
	MaxTimeAdjustment time.Duration

	subcmdMode bool
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
	if v.PollOrdersRetryInterval == 0 {
		v.PollOrdersRetryInterval = time.Minute
	}
	if v.RetryCount == 0 {
		v.RetryCount = 3
	}
	if v.MaxWebsocketOutOfOrderAllowance == 0 {
		v.MaxWebsocketOutOfOrderAllowance = 10
	}
	if v.MaxTimeAdjustment == 0 {
		v.MaxTimeAdjustment = time.Minute
	}
}

func SubcommandOptions() *Options {
	return &Options{subcmdMode: true}
}
