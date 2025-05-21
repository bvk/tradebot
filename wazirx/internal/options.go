// Copyright (c) 2025 BVK Chaitanya

package internal

import "time"

var (
	RestHostname      = "api.wazirx.com"
	WebsocketHostname = "stream.wazirx.com"
)

type Options struct {
	// Hostnames for the REST and WebSocket service endpoints.
	RestHostname      string
	WebsocketHostname string

	// Timeout to use for the HTTP requests.
	HttpClientTimeout time.Duration

	// Max time latency for fetching the server time from coinbase.
	MaxFetchTimeLatency time.Duration

	// Max limit for time difference between local time and the server times.
	MaxTimeAdjustment time.Duration

	// ServerTimeRateLimitPerSecond is number of check server time calls allowed
	// per second.
	ServerTimeRateLimitPerSecond int
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
	if v.MaxFetchTimeLatency == 0 {
		v.MaxFetchTimeLatency = 5 * time.Second
	}
	if v.MaxTimeAdjustment == 0 {
		v.MaxTimeAdjustment = time.Minute
	}
	if v.ServerTimeRateLimitPerSecond == 0 {
		v.ServerTimeRateLimitPerSecond = 1
	}
}

func (v *Options) Check() error {
	v.setDefaults()
	return nil
}
