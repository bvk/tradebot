// Copyright (c) 2023 BVK Chaitanya

package subcmds

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/bvk/tradebot/cli"
	"github.com/bvk/tradebot/server"
	"github.com/bvkgo/kvbadger"
	"github.com/dgraph-io/badger/v4"
	"github.com/nightlyone/lockfile"
)

type Fix struct {
	secretsPath string
	dataDir     string
}

func (c *Fix) Synopsis() string {
	return "Fix runs the Fix api on all trades"
}

func (c *Fix) Command() (*flag.FlagSet, cli.CmdFunc) {
	fset := flag.NewFlagSet("fix", flag.ContinueOnError)
	fset.StringVar(&c.secretsPath, "secrets-file", "", "path to credentials file")
	fset.StringVar(&c.dataDir, "data-dir", "", "path to the data directory")
	return fset, cli.CmdFunc(c.run)
}

func (c *Fix) run(ctx context.Context, args []string) error {
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	if len(c.dataDir) == 0 {
		c.dataDir = filepath.Join(os.Getenv("HOME"), ".tradebot")
	}
	if _, err := os.Stat(c.dataDir); err != nil {
		return fmt.Errorf("could not stat data directory %q: %w", c.dataDir, err)
	}
	dataDir, err := filepath.Abs(c.dataDir)
	if err != nil {
		return fmt.Errorf("could not determine data-dir %q absolute path: %w", c.dataDir, err)
	}

	if len(c.secretsPath) == 0 {
		c.secretsPath = filepath.Join(dataDir, "secrets.json")
	}
	secrets, err := server.SecretsFromFile(c.secretsPath)
	if err != nil {
		return err
	}

	lockPath := filepath.Join(dataDir, "tradebot.lock")
	flock, err := lockfile.New(lockPath)
	if err != nil {
		return fmt.Errorf("could not create lock file %q: %w", lockPath, err)
	}
	if err := flock.TryLock(); err != nil {
		return fmt.Errorf("could not get lock on file %q: %w", lockPath, err)
	}
	defer flock.Unlock()

	// Open the database.
	bopts := badger.DefaultOptions(dataDir)
	bdb, err := badger.Open(bopts)
	if err != nil {
		return fmt.Errorf("could not open the database: %w", err)
	}
	defer bdb.Close()
	db := kvbadger.New(bdb, isGoodKey)

	// Start other services.
	topts := &server.Options{
		NoResume: true,
		RunFixes: true,
	}
	trader, err := server.New(secrets, db, topts)
	if err != nil {
		return err
	}
	defer trader.Close()

	if err := trader.Start(ctx); err != nil {
		return err
	}
	if err := trader.Stop(ctx); err != nil {
		return err
	}
	return nil
}
