// Copyright (c) 2023 BVK Chaitanya

package trader

type Options struct {
	// RunFixes when true, trader.Start method will call Fix method on all trade
	// jobs (irrespective of their job status).
	RunFixes bool

	// NoResume when true, will NOT resume the trade jobs automatically.
	NoResume bool
}
