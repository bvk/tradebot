// Copyright (c) 2023 BVK Chaitanya

package exchange

import (
	"bytes"
	"context"
	"encoding/gob"
	"flag"
	"fmt"
	"log"
	"path"
	"time"

	"github.com/bvk/tradebot/api"
	"github.com/bvk/tradebot/cli"
	"github.com/bvk/tradebot/gobs"
	"github.com/bvk/tradebot/server"
	"github.com/bvk/tradebot/subcmds/cmdutil"
	"github.com/bvkgo/kv"
)

type GetCandles struct {
	cmdutil.DBFlags

	exchange string
	product  string
	fromDate string
	numDays  int

	updateDB bool
}

func (c *GetCandles) Command() (*flag.FlagSet, cli.CmdFunc) {
	fset := flag.NewFlagSet("get-candles", flag.ContinueOnError)
	c.DBFlags.SetFlags(fset)
	fset.StringVar(&c.exchange, "exchange", "coinbase", "name of the exchange")
	fset.StringVar(&c.product, "product", "", "name of the trading pair")
	fset.StringVar(&c.fromDate, "from-date", "", "date of the day in YYYY-MM-DD format")
	fset.IntVar(&c.numDays, "num-days", 1, "number of the days from the date")
	fset.BoolVar(&c.updateDB, "update-db", false, "when true, existing candles db data are ignored")
	return fset, cli.CmdFunc(c.run)
}

func (c *GetCandles) fetchCandles(ctx context.Context, start, end time.Time) ([]*gobs.Candle, error) {
	var candles []*gobs.Candle
	req := &api.ExchangeGetCandlesRequest{
		ExchangeName: c.exchange,
		ProductID:    c.product,
		StartTime:    start,
		EndTime:      end,
	}
	for {
		resp, err := cmdutil.Post[api.ExchangeGetCandlesResponse](ctx, &c.DBFlags.ClientFlags, api.ExchangeGetCandlesPath, req)
		if err != nil {
			return nil, fmt.Errorf("POST request to get-candles failed: %w", err)
		}
		candles = append(candles, resp.Candles...)
		if resp.Continue == nil {
			break
		}
		req = resp.Continue
	}
	for i, c := range candles {
		if !c.StartTime.Before(end) {
			candles = candles[:i]
			break
		}
	}
	return candles, nil
}

func (c *GetCandles) run(ctx context.Context, args []string) error {
	if len(args) != 0 {
		return fmt.Errorf("this command takes no arguments")
	}

	if len(c.product) == 0 {
		return fmt.Errorf("product argument is required")
	}
	if len(c.fromDate) == 0 {
		return fmt.Errorf("date argument is required")
	}
	start, err := time.Parse("2006-01-02", c.fromDate)
	if err != nil {
		return fmt.Errorf("could not parse date argument: %w", err)
	}

	db, closer, err := c.DBFlags.GetDatabase(ctx)
	if err != nil {
		return fmt.Errorf("could not create database client: %w", err)
	}
	defer closer()

	for i := 0; i < c.numDays; i++ {
		end := start.Add(24 * time.Hour)
		if !c.updateDB {
			if _, err := LoadCandles(ctx, db, c.exchange, c.product, start); err == nil {
				log.Printf("candles for %s are already in the database", start.Format("2006-01-02"))
				start = end
				continue
			}
		}

		candles, err := c.fetchCandles(ctx, start, end)
		if err != nil {
			return fmt.Errorf("could not fetch candles: %w", err)
		}

		save := func(ctx context.Context, rw kv.ReadWriter) error {
			var buf bytes.Buffer
			enc := gob.NewEncoder(&buf)
			if err := enc.Encode(&gobs.Candles{Candles: candles}); err != nil {
				return fmt.Errorf("could not gob encode candles: %w", err)
			}

			key := path.Join(server.CandlesKeyspace, c.exchange, c.product, start.Format("2006-01-02"))
			return rw.Set(ctx, key, &buf)
		}
		if err := kv.WithReadWriter(ctx, db, save); err != nil {
			return fmt.Errorf("could not save to database: %w", err)
		}

		log.Printf("candles for %s are fetched and saved in the database", start.Format("2006-01-02"))
		start = end
	}

	// TODO: Print some candle analysis?

	return nil
}
