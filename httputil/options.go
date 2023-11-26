// Copyright (c) 2023 BVK Chaitanya

package httputil

import (
	"time"
)

type Options struct {
	// ServerCheckTimeout holds the http client timeout when checking for the
	// http server initialization.
	ServerCheckTimeout time.Duration

	// ServerCheckRetryInterval holds the amount of time to wait to check for
	// the http server readiness.
	ServerCheckRetryInterval time.Duration
}

func (v *Options) setDefaults() {
	if v.ServerCheckTimeout == 0 {
		v.ServerCheckTimeout = 10 * time.Second
	}
	if v.ServerCheckRetryInterval == 0 {
		v.ServerCheckRetryInterval = time.Second
	}
}

func (v *Options) Check() error {
	return nil
}
