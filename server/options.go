// Copyright (c) 2023 BVK Chaitanya

package server

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type Options struct {
	// RunFixes when true, trader.Start method will call Fix method on all trade
	// jobs (irrespective of their job status).
	RunFixes bool

	// Path to the data directory.
	DataDir string

	// BinaryBackupPath if non-empty holds path to the backup for the currently
	// executing binary path.
	BinaryBackupPath string

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

func (v *Options) Check() error {
	if len(v.BinaryBackupPath) != 0 {
		if !filepath.IsAbs(v.BinaryBackupPath) {
			return fmt.Errorf("binary backup path must be an absolute path")
		}
		if stat, err := os.Stat(v.BinaryBackupPath); err != nil {
			return err
		} else if !stat.Mode().IsRegular() {
			return fmt.Errorf("binary backup path must be a regular file")
		}
	}
	if len(v.DataDir) != 0 {
		if stat, err := os.Stat(v.DataDir); err != nil {
			return err
		} else if !stat.Mode().IsDir() {
			return fmt.Errorf("data dir path must be a directory")
		}
	}
	return nil
}
