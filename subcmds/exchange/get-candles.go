// Copyright (c) 2023 BVK Chaitanya

package exchange

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"time"

	"github.com/bvk/tradebot/api"
	"github.com/bvk/tradebot/cli"
	"github.com/bvk/tradebot/subcmds"
)

type GetCandles struct {
	subcmds.ClientFlags

	name    string
	product string

	start string
	end   string
}

func (c *GetCandles) Command() (*flag.FlagSet, cli.CmdFunc) {
	fset := flag.NewFlagSet("get-candles", flag.ContinueOnError)
	c.ClientFlags.SetFlags(fset)
	fset.StringVar(&c.name, "name", "coinbase", "name of the exchange")
	fset.StringVar(&c.product, "product", "BCH-USD", "name of the trading pair")
	fset.StringVar(&c.start, "start", "", "start time for the candles")
	fset.StringVar(&c.end, "end", "", "end time for the candles")
	return fset, cli.CmdFunc(c.run)
}

func (c *GetCandles) run(ctx context.Context, args []string) error {
	if len(args) != 0 {
		return fmt.Errorf("this command takes no arguments")
	}

	if c.start == "" {
		return fmt.Errorf("start time flag is required")
	}
	startTime, err := time.Parse(time.RFC3339, c.start)
	if err != nil {
		return fmt.Errorf("could not parse start time as RFC3339 value: %w", err)
	}

	endTime := time.Now()
	if c.end != "" {
		v, err := time.Parse(time.RFC3339, c.end)
		if err != nil {
			return fmt.Errorf("could not parse end time as RFC3339 value: %w", err)
		}
		endTime = v
	}

	req := &api.ExchangeGetCandlesRequest{
		ExchangeName: c.name,
		ProductID:    c.product,
		StartTime:    startTime,
		EndTime:      endTime,
	}
	for {
		resp, err := subcmds.Post[api.ExchangeGetCandlesResponse](ctx, &c.ClientFlags, "/exchange/get-candles", req)
		if err != nil {
			return fmt.Errorf("POST request to exchange/get-order failed: %w", err)
		}

		jsdata, _ := json.MarshalIndent(resp, "", "  ")
		fmt.Printf("%s\n", jsdata)

		if resp.Continue == nil {
			break
		}
		req = resp.Continue
	}

	return nil
}
