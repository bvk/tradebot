// Copyright (c) 2023 BVK Chaitanya

package server

import "time"

type Options struct {
	// RunFixes when true, trader.Start method will call Fix method on all trade
	// jobs (irrespective of their job status).
	RunFixes bool

	// NoResume when true, will NOT resume the trade jobs automatically.
	NoResume bool

	// NoFetchCandles when true, will disable periodic fetch of product candles data.
	NoFetchCandles bool

	// Max time latency for fetching the server time from coinbase.
	MaxFetchTimeLatency time.Duration

	// Max timeout for http requests.
	MaxHttpClientTimeout time.Duration
}

func (v *Options) setDefaults() {
	if v.MaxFetchTimeLatency == 0 {
		v.MaxFetchTimeLatency = time.Second
	}
	if v.MaxHttpClientTimeout == 0 {
		v.MaxHttpClientTimeout = 10 * time.Second
	}
}
