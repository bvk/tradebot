// Copyright (c) 2023 BVK Chaitanya

package db

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"time"

	"github.com/bvkgo/kv"
	"github.com/bvkgo/kv/kvhttp"
	"github.com/bvkgo/kv/kvmemdb"
	"github.com/bvkgo/kvbadger"
	"github.com/dgraph-io/badger/v4"
)

type Flags struct {
	port        int
	ip          string
	basePath    string
	httpTimeout time.Duration

	dataDir string

	fromBackup string
}

func (f *Flags) check() error {
	// TODO: Add checks.
	return nil
}

func (f *Flags) SetFlags(fset *flag.FlagSet) {
	fset.StringVar(&f.dataDir, "data-dir", "", "Path to the database directory")

	fset.StringVar(&f.fromBackup, "from-backup", "", "Path to a database backup file")

	fset.IntVar(&f.port, "port", 10000, "TCP port number for the db endpoint")
	fset.StringVar(&f.ip, "ip", "127.0.0.1", "TCP ip address for the db endpoint")
	fset.StringVar(&f.basePath, "base-path", "/db", "path to db api handler")
	fset.DurationVar(&f.httpTimeout, "http-timeout", 1*time.Second, "http client timeout")
}

func (f *Flags) GetDatabase(ctx context.Context) (kv.Database, error) {
	isGoodKey := func(k string) bool {
		return path.IsAbs(k) && k == path.Clean(k)
	}

	if len(f.fromBackup) != 0 {
		fp, err := os.Open(f.fromBackup)
		if err != nil {
			return nil, fmt.Errorf("could not open file %q: %w", f.fromBackup, err)
		}
		defer fp.Close()

		r := bufio.NewReader(fp)

		db := kvmemdb.New()
		if err := doRestore(ctx, r, db); err != nil {
			return nil, fmt.Errorf("could not restore in-memory db from backup: %w", err)
		}
		return db, nil
	}

	if len(f.dataDir) != 0 {
		bopts := badger.DefaultOptions(f.dataDir)
		bdb, err := badger.Open(bopts)
		if err != nil {
			return nil, fmt.Errorf("could not open the database: %w", err)
		}
		return kvbadger.New(bdb, isGoodKey), nil
	}

	baseURL := &url.URL{
		Scheme: "http",
		Host:   net.JoinHostPort(f.ip, fmt.Sprintf("%d", f.port)),
		Path:   f.basePath,
	}
	client := &http.Client{
		Timeout: f.httpTimeout,
	}

	return kvhttp.New(baseURL, client), nil
}
