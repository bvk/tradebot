// Copyright (c) 2025 BVK Chaitanya

package internal

import "net/url"

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
	// URLs for the REST and WebSocket service endpoints.
	RestURL      string
	WebsocketURL string
}

func (v *Options) setDefaults() {
	if v.RestURL == "" {
		v.RestURL = RestURL.String()
	}
	if v.WebsocketURL == "" {
		v.WebsocketURL = WebsocketURL.String()
	}
}

// Check validates the options.
func (v *Options) Check() error {
	return nil
}
