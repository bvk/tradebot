// Copyright (c) 2023 BVK Chaitanya

package server

import (
	"net"
	"time"
)

type Options struct {
	ListenIP   net.IP
	ListenPort int

	// ServerCheckTimeout holds the http client timeout when checking for the
	// http server initialization.
	ServerCheckTimeout time.Duration

	// ServerCheckRetryInterval holds the amount of time to wait to check for
	// the http server readiness.
	ServerCheckRetryInterval time.Duration
}

func (v *Options) setDefaults() {
	if v.ListenIP == nil {
		v.ListenIP = net.IPv4(127, 0, 0, 1)
	}
	if v.ServerCheckRetryInterval == 0 {
		v.ServerCheckRetryInterval = time.Second
	}
}

func (v *Options) Check() error {
	return nil
}
