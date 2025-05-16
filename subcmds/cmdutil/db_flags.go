// Copyright (c) 2023 BVK Chaitanya

package cmdutil

import (
	"bufio"
	"bytes"
	"context"
	"encoding/gob"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path"
	"time"

	"github.com/bvk/tradebot/gobs"
	"github.com/bvk/tradebot/kvutil"
	"github.com/bvkgo/kv"
	"github.com/bvkgo/kv/kvhttp"
	"github.com/bvkgo/kv/kvmemdb"
	"github.com/bvkgo/kvbadger"
	"github.com/dgraph-io/badger/v4"
)

type DBFlags struct {
	ClientFlags

	dbURLPath   string
	httpTimeout time.Duration

	dataDir string

	fromBackup string

	backupBefore string
	backupAfter  string
}

func (f *DBFlags) check() error {
	// TODO: Add checks.
	return nil
}

func (f *DBFlags) SetFlags(fset *flag.FlagSet) {
	fset.StringVar(&f.dataDir, "data-dir", "", "Path to the database directory")

	fset.StringVar(&f.fromBackup, "from-backup", "", "Path to a database backup file")

	f.ClientFlags.SetFlags(fset)
	fset.StringVar(&f.dbURLPath, "db-url-path", "/db", "path to db api handler")

	fset.StringVar(&f.backupBefore, "backup-before", "", "Path to a file to receive db backup before cmd is run")
	fset.StringVar(&f.backupAfter, "backup-after", "", "Path to a file to receive db backup after cmd is run")
}

func (f *DBFlags) dbCloser(db kv.Database, c io.Closer) func() {
	return func() {
		if len(f.backupAfter) != 0 {
			if err := kvutil.BackupDB(context.Background(), db, f.backupAfter); err != nil {
				log.Printf("could not take db backup after it is used (ignored): %v", err)
			}
		}
		if c != nil {
			c.Close()
		}
	}
}

// IsRemoteDatabase returns true if target database is a remote database over
// http.
func (f *DBFlags) IsRemoteDatabase() bool {
	return f.fromBackup == "" && f.dataDir == ""
}

func (f *DBFlags) GetDatabase(ctx context.Context) (db kv.Database, closer func(), status error) {
	defer func() {
		if status == nil && len(f.backupBefore) != 0 {
			if err := kvutil.BackupDB(ctx, db, f.backupBefore); err != nil {
				log.Printf("could not take a db backup before it is used: %v", err)
				db, closer, status = nil, nil, err
			}
		}
	}()

	isGoodKey := func(k string) bool {
		return path.IsAbs(k) && k == path.Clean(k)
	}

	if len(f.fromBackup) != 0 {
		fp, err := os.Open(f.fromBackup)
		if err != nil {
			return nil, nil, fmt.Errorf("could not open file %q: %w", f.fromBackup, err)
		}
		defer fp.Close()

		r := bufio.NewReader(fp)

		db := kvmemdb.New()
		if err := doRestore(ctx, r, db); err != nil {
			return nil, nil, fmt.Errorf("could not restore in-memory db from backup: %w", err)
		}
		return db, f.dbCloser(db, nil), nil
	}

	if len(f.dataDir) != 0 {
		bopts := badger.DefaultOptions(f.dataDir)
		bdb, err := badger.Open(bopts)
		if err != nil {
			return nil, nil, fmt.Errorf("could not open the database: %w", err)
		}
		db := kvbadger.New(bdb, isGoodKey)
		return db, f.dbCloser(db, nil), nil
	}

	addrURL := f.ClientFlags.AddressURL()
	addrURL.Path = path.Join(addrURL.Path, f.dbURLPath)
	db = kvhttp.New(addrURL, f.ClientFlags.HttpClient())
	return db, f.dbCloser(db, nil), nil
}

func doRestore(ctx context.Context, r io.Reader, db kv.Database) error {
	decoder := gob.NewDecoder(r)
	restore := func(ctx context.Context, w kv.ReadWriter) error {
		it, err := w.Scan(ctx)
		if err != nil {
			return fmt.Errorf("could not create scanning iterator: %w", err)
		}
		defer kv.Close(it)
		for k, _, err := it.Fetch(ctx, false); err == nil; k, _, err = it.Fetch(ctx, true) {
			if err := w.Delete(ctx, k); err != nil {
				return fmt.Errorf("could not delete key %q: %w", k, err)
			}
		}
		if _, _, err := it.Fetch(ctx, false); err != nil && !errors.Is(err, io.EOF) {
			return fmt.Errorf("iterator fetch has failed: %w", err)
		}

		var item gobs.KeyValue
		for err = decoder.Decode(&item); err == nil; err = decoder.Decode(&item) {
			if err := w.Set(ctx, item.Key, bytes.NewReader(item.Value)); err != nil {
				return fmt.Errorf("could not restore at key %q: %w", item.Key, err)
			}
			item = gobs.KeyValue{}
		}

		if !errors.Is(err, io.EOF) {
			return fmt.Errorf("could not decode item from backup file: %w", err)
		}
		return nil
	}

	if err := kv.WithReadWriter(ctx, db, restore); err != nil {
		return fmt.Errorf("could not run restore with a transaction: %w", err)
	}
	return nil
}
