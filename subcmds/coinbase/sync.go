// Copyright (c) 2023 BVK Chaitanya

package coinbase

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"slices"
	"syscall"
	"time"

	"github.com/bvk/tradebot/cli"
	"github.com/bvk/tradebot/coinbase"
	"github.com/bvk/tradebot/server"
	"github.com/bvk/tradebot/subcmds/cmdutil"
)

type Sync struct {
	cmdutil.DBFlags

	secretsPath string

	fromDate string

	dataType string

	productID string
}

func (c *Sync) Command() (*flag.FlagSet, cli.CmdFunc) {
	fset := flag.NewFlagSet("sync", flag.ContinueOnError)
	c.DBFlags.SetFlags(fset)
	fset.StringVar(&c.productID, "product-id", "", "product id")
	fset.StringVar(&c.fromDate, "from-date", "", "date of the day in YYYY-MM-DD format")
	fset.StringVar(&c.secretsPath, "secrets-file", "", "path to credentials file")
	fset.StringVar(&c.dataType, "data-type", "", "one of filled|canceled|candles")
	return fset, cli.CmdFunc(c.run)
}

func (c *Sync) run(ctx context.Context, args []string) error {
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	dataTypes := []string{
		"filled", "canceled", "cancelled", "candles",
	}
	if !slices.Contains(dataTypes, c.dataType) {
		return fmt.Errorf("data-type must be one of %v", dataTypes)
	}

	if len(c.secretsPath) == 0 {
		return fmt.Errorf("secrets file is required")
	}
	secrets, err := server.SecretsFromFile(c.secretsPath)
	if err != nil {
		return fmt.Errorf("could not load secrets: %w", err)
	}
	if secrets.Coinbase == nil {
		return fmt.Errorf("coinbase credentials are missing")
	}

	if len(c.fromDate) == 0 {
		return errors.New("date argument is required")
	}
	from, err := time.Parse("2006-01-02", c.fromDate)
	if err != nil {
		return fmt.Errorf("could not parse date argument: %w", err)
	}

	db, closer, err := c.DBFlags.GetDatabase(ctx)
	if err != nil {
		return fmt.Errorf("could not create database client: %w", err)
	}
	defer closer()

	opts := coinbase.SubcommandOptions()
	exchange, err := coinbase.New(ctx, db, secrets.Coinbase.Key, secrets.Coinbase.Secret, opts)
	if err != nil {
		return fmt.Errorf("could not create coinbase client: %w", err)
	}
	defer exchange.Close()

	switch c.dataType {
	case "filled":
		return exchange.SyncFilled(ctx, from.UTC())
	case "canceled", "cancelled":
		return exchange.SyncCancelled(ctx, from.UTC())
	case "candles":
		if c.productID == "" {
			return fmt.Errorf("product id cannot be empty for syncing candles")
		}
		end := time.Now().Truncate(time.Minute)
		return exchange.SyncCandles(ctx, c.productID, from.UTC(), end)
	default:
		return fmt.Errorf("unsupported coinbase data type %q", c.dataType)
	}
}
