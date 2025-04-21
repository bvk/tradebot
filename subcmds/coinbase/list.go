// Copyright (c) 2023 BVK Chaitanya

package coinbase

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"slices"
	"syscall"
	"time"

	"github.com/bvk/tradebot/coinbase"
	"github.com/bvk/tradebot/gobs"
	"github.com/bvk/tradebot/subcmds/cmdutil"
	"github.com/visvasity/cli"
)

type List struct {
	cmdutil.DBFlags

	beginDate, endDate string

	dataType string

	productID string
}

func (c *List) Command() (string, *flag.FlagSet, cli.CmdFunc) {
	fset := flag.NewFlagSet("list", flag.ContinueOnError)
	c.DBFlags.SetFlags(fset)
	fset.StringVar(&c.productID, "product-id", "", "product id")
	fset.StringVar(&c.beginDate, "begin-date", "", "date of start day in YYYY-MM-DD format")
	fset.StringVar(&c.endDate, "end-date", "", "date of stop day in YYYY-MM-DD format")
	fset.StringVar(&c.dataType, "data-type", "", "one of orders|candles")
	return "list", fset, cli.CmdFunc(c.run)
}

func (c *List) run(ctx context.Context, args []string) error {
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	dataTypes := []string{
		"orders", "candles",
	}
	if !slices.Contains(dataTypes, c.dataType) {
		return fmt.Errorf("data-type must be one of %v", dataTypes)
	}

	if c.productID == "" {
		return fmt.Errorf("product id cannot be empty for syncing candles")
	}

	if len(c.beginDate) == 0 {
		return errors.New("begin-date argument is required")
	}
	begin, err := time.Parse("2006-01-02", c.beginDate)
	if err != nil {
		return fmt.Errorf("could not parse date argument: %w", err)
	}
	end := time.Now()
	if len(c.endDate) > 0 {
		v, err := time.Parse("2006-01-02", c.endDate)
		if err != nil {
			return fmt.Errorf("could not parse end date argument: %w", err)
		}
		end = v
	}

	db, closer, err := c.DBFlags.GetDatabase(ctx)
	if err != nil {
		return fmt.Errorf("could not create database client: %w", err)
	}
	defer closer()

	ds := coinbase.NewDatastore(db)

	print := func(v any) error {
		js, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			return err
		}
		fmt.Printf("%s\n", js)
		return nil
	}

	switch c.dataType {
	case "filled":
		fmt.Println("TODO")
		return nil
	case "canceled", "cancelled":
		fmt.Println("TODO")
		return nil

	case "candles":
		return ds.ScanCandles(ctx, c.productID, begin, end, func(v *gobs.Candle) error {
			return print(v)
		})

	default:
		return fmt.Errorf("unsupported coinbase data type %q", c.dataType)
	}
}
