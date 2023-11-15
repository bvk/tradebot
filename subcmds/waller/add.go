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

	product string

	spec Spec
}

func (c *Add) check() error {
	if err := c.spec.Check(); err != nil {
		return err
	}
	return nil
}

func (c *Add) buySellPoints() [][2]*point.Point {
	pairs := fixedProfitPairs(&c.spec)
	var points [][2]*point.Point
	for i := range pairs {
		bs := [2]*point.Point{
			{
				Size:   pairs[i].Buy.Size,
				Price:  pairs[i].Buy.Price.Truncate(2),
				Cancel: pairs[i].Buy.Cancel.Truncate(2),
			},
			{
				Size:   pairs[i].Sell.Size,
				Price:  pairs[i].Sell.Price.Truncate(2),
				Cancel: pairs[i].Sell.Cancel.Truncate(2),
			},
		}
		points = append(points, bs)
	}
	return points
}

func (c *Add) Run(ctx context.Context, args []string) error {
	if len(args) != 0 {
		return fmt.Errorf("this command takes no arguments")
	}
	if err := c.check(); err != nil {
		return err
	}

	points := c.buySellPoints()
	if points == nil {
		return fmt.Errorf("could not determine buy/sell points")
	}

	if c.dryRun {
		for i, p := range points {
			d0, _ := json.Marshal(p[0])
			fmt.Printf("buy-%d:  %s\n", i, d0)
			d1, _ := json.Marshal(p[1])
			fmt.Printf("sell-%d: %s\n", i, d1)
		}
		return nil
	}

	req := &api.WallRequest{
		Product:       c.product,
		BuySellPoints: points,
	}
	resp, err := subcmds.Post[api.WallResponse](ctx, &c.ClientFlags, "/trader/wall", req)
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
	fset.StringVar(&c.product, "product", "BCH-USD", "product id for the trade")
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
