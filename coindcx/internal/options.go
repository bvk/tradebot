// Copyright (c) 2025 BVK Chaitanya

package internal

import "time"

var (
	RestHostname      = "api.coindcx.com"
	WebsocketHostname = "TODO"
)

type Options struct {
	// Hostnames for the REST and WebSocket service endpoints.
	RestHostname      string
	WebsocketHostname string

	// Timeout to use for the HTTP requests.
	HttpClientTimeout time.Duration
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
}

func (v *Options) Check() error {
	v.setDefaults()
	return nil
}
