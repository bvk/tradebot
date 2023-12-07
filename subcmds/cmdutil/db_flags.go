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
	"os"
	"path"
	"strings"
	"time"

	"github.com/bvk/tradebot/gobs"
	"github.com/bvk/tradebot/namer"
	"github.com/bvk/tradebot/server"
	"github.com/bvkgo/kv"
	"github.com/bvkgo/kv/kvhttp"
	"github.com/bvkgo/kv/kvmemdb"
	"github.com/bvkgo/kvbadger"
	"github.com/dgraph-io/badger/v4"
	"github.com/google/uuid"
)

type DBFlags struct {
	ClientFlags

	dbURLPath   string
	httpTimeout time.Duration

	dataDir string

	fromBackup string
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
}

func (f *DBFlags) GetDatabase(ctx context.Context) (kv.Database, error) {
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

func (f *DBFlags) ResolveName(ctx context.Context, arg string) (string, string, error) {
	name := arg
	if strings.HasPrefix(arg, "name:") {
		name = strings.TrimPrefix(arg, "name:")
	}
	db, err := f.GetDatabase(ctx)
	if err != nil {
		return "", "", fmt.Errorf("could not create database client: %w", err)
	}
	var id, typename string
	resolve := func(ctx context.Context, r kv.Reader) error {
		a, b, err := namer.ResolveName(ctx, r, name)
		if err != nil {
			return fmt.Errorf("could not resolve name %q: %w", arg, err)
		}
		id, typename = a, b
		return nil
	}
	if err := kv.WithReader(ctx, db, resolve); err != nil {
		return "", "", fmt.Errorf("could not resolve name: %w", err)
	}
	return id, typename, nil
}

func (f *DBFlags) GetJobID(ctx context.Context, arg string) (uid string, err error) {
	prefix, value := "", ""
	switch {
	case strings.HasPrefix(arg, "uuid:"):
		prefix = "uuid"
		value = strings.TrimPrefix(arg, "uuid:")
	case strings.HasPrefix(arg, "key:"):
		prefix = "key"
		value = strings.TrimPrefix(arg, "key:")
	case strings.HasPrefix(arg, "name:"):
		prefix = "name"
		value = strings.TrimPrefix(arg, "name:")
	}
	if prefix == "uuid" {
		if _, err := uuid.Parse(value); err != nil {
			return "", fmt.Errorf("could not parse argument %q to uuid: %w", arg, err)
		}
		return value, nil
	}
	if prefix == "key" {
		if !path.IsAbs(value) {
			return "", os.ErrInvalid
		}
		dir, file := path.Split(value)
		if dir != server.JobsKeyspace {
			return "", fmt.Errorf("argument must be from %q keyspace", server.JobsKeyspace)
		}
		if _, err := uuid.Parse(file); err != nil {
			return "", fmt.Errorf("could not parse base component %q to uuid: %w", file, err)
		}
		return file, nil
	}
	if prefix == "name" {
		id, _, err := f.ResolveName(ctx, value)
		if err != nil {
			return "", fmt.Errorf("could not resolve name prefixed argument %q: %w", value, err)
		}
		return id, nil
	}
	// Automatic UUID resolution.
	if _, err := uuid.Parse(arg); err == nil {
		return arg, nil
	}
	// Automatic key resolution.
	if path.IsAbs(arg) {
		dir, file := path.Split(arg)
		if dir != server.JobsKeyspace {
			return "", fmt.Errorf("argument must be from %q keyspace", server.JobsKeyspace)
		}
		if _, err := uuid.Parse(file); err != nil {
			return "", fmt.Errorf("could not parse base component %q to uuid: %w", file, err)
		}
		return file, nil
	}
	// Automatic name resolution.
	if id, _, err := f.ResolveName(ctx, arg); err == nil {
		return id, nil
	}
	return "", fmt.Errorf("could not convert/resolve argument %q to an uuid", arg)
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
