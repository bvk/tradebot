// Copyright (c) 2025 BVK Chaitanya

package watcher

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"

	"github.com/bvk/tradebot/api"
	"github.com/bvk/tradebot/subcmds/cmdutil"
	"github.com/bvk/tradebot/subcmds/waller"
	"github.com/visvasity/cli"
)

type Add struct {
	cmdutil.ClientFlags

	dryRun bool

	product  string
	exchange string
	name     string

	spec waller.Spec
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
	return nil
}

func (c *Add) Run(ctx context.Context, args []string) error {
	if len(args) != 0 {
		return fmt.Errorf("this command takes no arguments")
	}
	if err := c.check(); err != nil {
		return err
	}

	pairs := c.spec.BuySellPairs()
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

	req1 := &api.WatchRequest{
		ProductID:    c.product,
		ExchangeName: c.exchange,
		Pairs:        pairs,
		FeePct:       c.spec.FeePct(),
	}
	resp1, err := cmdutil.Post[api.WatchResponse](ctx, &c.ClientFlags, api.WatchPath, req1)
	if err != nil {
		return err
	}

	req2 := &api.SetJobNameRequest{
		UID:     resp1.UID,
		JobName: c.name,
	}
	if _, err := cmdutil.Post[api.SetJobNameResponse](ctx, &c.ClientFlags, api.SetJobNamePath, req2); err != nil {
		log.Printf("job is created, but could not set the job name (ignored): %v", err)
	}

	jsdata, _ := json.MarshalIndent(resp1, "", "  ")
	fmt.Printf("%s\n", jsdata)
	return nil
}

func (c *Add) Command() (string, *flag.FlagSet, cli.CmdFunc) {
	fset := flag.NewFlagSet("add", flag.ContinueOnError)
	c.ClientFlags.SetFlags(fset)
	c.spec.SetFlags(fset)
	fset.BoolVar(&c.dryRun, "dry-run", false, "when true only prints the trade points")
	fset.StringVar(&c.name, "name", "", "a name for the trader job")
	fset.StringVar(&c.product, "product", "", "product id for the trader")
	fset.StringVar(&c.exchange, "exchange", "coinbase", "exchange name for the product")
	return "add", fset, cli.CmdFunc(c.Run)
}

func (c *Add) Purpose() string {
	return "Creates a new watcher job over a price range"
}

func (c *Add) Description() string {
	return `

Command "add" creates a watcher job that simulates a waller job over real-time
price movements from an exchange. See waller job's documentation for more
details.

`
}
