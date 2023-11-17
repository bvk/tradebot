// Copyright (c) 2023 BVK Chaitanya

package db

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"path"
	"strings"
	"time"

	"github.com/bvk/tradebot/subcmds"
	"github.com/bvk/tradebot/trader"
	"github.com/bvkgo/kv"
	"github.com/bvkgo/kv/kvhttp"
	"github.com/bvkgo/kv/kvmemdb"
	"github.com/bvkgo/kvbadger"
	"github.com/dgraph-io/badger/v4"
	"github.com/google/uuid"
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

func (f *Flags) ResolveName(ctx context.Context, arg string) (string, error) {
	name := arg
	if strings.HasPrefix(arg, "name:") {
		name = strings.TrimPrefix(arg, "name:")
	}
	db, err := f.GetDatabase(ctx)
	if err != nil {
		return "", fmt.Errorf("could not create database client: %w", err)
	}
	v, err := trader.ResolveName(ctx, db, name)
	if err != nil {
		return "", fmt.Errorf("could not resolve name %q: %w", arg, err)
	}
	return v, nil
}

func (f *Flags) GetJobID(ctx context.Context, arg string) (uid string, err error) {
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
		if dir != trader.JobsKeyspace {
			return "", fmt.Errorf("argument must be from %q keyspace", trader.JobsKeyspace)
		}
		if _, err := uuid.Parse(file); err != nil {
			return "", fmt.Errorf("could not parse base component %q to uuid: %w", file, err)
		}
		return file, nil
	}
	if prefix == "name" {
		data, err := f.ResolveName(ctx, value)
		if err != nil {
			return "", fmt.Errorf("could not resolve name prefixed argument %q: %w", value, err)
		}
		dir, file := path.Split(data)
		if dir != trader.JobsKeyspace {
			return "", fmt.Errorf("argument must be from %q keyspace", trader.JobsKeyspace)
		}
		if _, err := uuid.Parse(file); err != nil {
			return "", fmt.Errorf("could not parse base component %q to uuid: %w", file, err)
		}
		return file, nil
	}
	// Automatic UUID resolution.
	if _, err := uuid.Parse(arg); err == nil {
		return arg, nil
	}
	// Automatic key resolution.
	if path.IsAbs(arg) {
		dir, file := path.Split(arg)
		if dir != trader.JobsKeyspace {
			return "", fmt.Errorf("argument must be from %q keyspace", trader.JobsKeyspace)
		}
		if _, err := uuid.Parse(file); err != nil {
			return "", fmt.Errorf("could not parse base component %q to uuid: %w", file, err)
		}
		return file, nil
	}
	// Automatic name resolution.
	if data, err := f.ResolveName(ctx, arg); err == nil {
		dir, file := path.Split(data)
		if dir != trader.JobsKeyspace {
			return "", fmt.Errorf("argument must be from %q keyspace", trader.JobsKeyspace)
		}
		if _, err := uuid.Parse(file); err != nil {
			return "", fmt.Errorf("could not parse base component %q to uuid: %w", file, err)
		}
		return file, nil
	}
	return "", fmt.Errorf("could not convert/resolve argument %q to an uuid", arg)
}
