// Copyright (c) 2023 BVK Chaitanya

package db

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"path"
	"time"

	"github.com/bvk/tradebot/subcmds"
	"github.com/bvkgo/kv"
	"github.com/bvkgo/kv/kvhttp"
	"github.com/bvkgo/kv/kvmemdb"
	"github.com/bvkgo/kvbadger"
	"github.com/dgraph-io/badger/v4"
)

type Flags struct {
	subcmds.ClientFlags

	dbURLPath   string
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

	f.ClientFlags.SetFlags(fset)
	fset.StringVar(&f.dbURLPath, "db-url-path", "/db", "path to db api handler")
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

	addrURL := f.ClientFlags.AddressURL()
	addrURL.Path = path.Join(addrURL.Path, f.dbURLPath)
	return kvhttp.New(addrURL, f.ClientFlags.HttpClient()), nil
}
