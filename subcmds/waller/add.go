// Copyright (c) 2023 BVK Chaitanya

package waller

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"

	"github.com/bvk/tradebot/api"
	"github.com/bvk/tradebot/cli"
	"github.com/bvk/tradebot/point"
	"github.com/bvk/tradebot/subcmds"
)

type Add struct {
	subcmds.ClientFlags

	dryRun bool

	product  string
	exchange string

	spec Spec
}

func (c *Add) check() error {
	if len(c.product) == 0 {
		return fmt.Errorf("product name cannot be empty")
	}
	if len(c.exchange) == 0 {
		return fmt.Errorf("exchange name cannot be empty")
	}
	if err := c.spec.Check(); err != nil {
		return err
	}
	if c.product == "" {
		return fmt.Errorf("product name must be specified")
	}
	return nil
}

func (c *Add) buySellPairs() []*point.Pair {
	return fixedProfitPairs(&c.spec)
}

func (c *Add) Run(ctx context.Context, args []string) error {
	if len(args) != 0 {
		return fmt.Errorf("this command takes no arguments")
	}
	if err := c.check(); err != nil {
		return err
	}

	pairs := c.buySellPairs()
	if pairs == nil {
		return fmt.Errorf("could not determine buy/sell points")
	}

	if c.dryRun {
		for i, p := range pairs {
			d0, _ := json.Marshal(p.Buy)
			fmt.Printf("buy-%d:  %s\n", i, d0)
			d1, _ := json.Marshal(p.Sell)
			fmt.Printf("sell-%d: %s\n", i, d1)
		}
		return nil
	}

	req := &api.WallRequest{
		ProductID:    c.product,
		ExchangeName: c.exchange,
		Pairs:        pairs,
	}
	resp, err := subcmds.Post[api.WallResponse](ctx, &c.ClientFlags, api.WallPath, req)
	if err != nil {
		return err
	}
	jsdata, _ := json.MarshalIndent(resp, "", "  ")
	fmt.Printf("%s\n", jsdata)
	return nil
}

func (c *Add) Command() (*flag.FlagSet, cli.CmdFunc) {
	fset := flag.NewFlagSet("add", flag.ContinueOnError)
	c.ClientFlags.SetFlags(fset)
	c.spec.SetFlags(fset)
	fset.BoolVar(&c.dryRun, "dry-run", false, "when true only prints the trade points")
	fset.StringVar(&c.product, "product", "", "product id for the trade")
	fset.StringVar(&c.exchange, "exchange", "coinbase", "exchange name for the product")
	return fset, cli.CmdFunc(c.Run)
}

func (c *Add) Synopsis() string {
	return "Creates a new buy-sell over a range job"
}

func (c *Add) CommandHelp() string {
	return `

Command "add" creates multiple buy-and-sell loops within a given ticker price
range (begin, end), so that as along as the ticker price is within the given
range, there will always be a buy or sell action following the ticker price.

Since (1) each sell point is associated with a buy point (2) sell point is
above it's associated buy point and (3) sell is performed only after it's
associated buy has completed every sell point execution generates a little
profit.

Note that when the ticker price goes completely above the chosen price-range,
then all sell points -- for already completed buys if any -- will be executed
and all buy points will be waiting for the ticker to come back down. Similarly,
when the ticker price goes completely below the chosen price-range then all buy
points will be executed, and sell points will be waiting for the ticker to come
back up.

`
}
