// Copyright (c) 2023 BVK Chaitanya

package looper

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"

	"github.com/bvk/tradebot/api"
	"github.com/bvk/tradebot/cli"
	"github.com/bvk/tradebot/point"
	"github.com/bvk/tradebot/subcmds"
	"github.com/shopspring/decimal"
)

type Add struct {
	subcmds.ClientFlags

	product string

	buySize         float64
	buyPrice        float64
	buyCancelOffset float64

	sellSize         float64
	sellPrice        float64
	sellCancelOffset float64
}

func (c *Add) check() error {
	if c.buySize <= 0 || c.sellSize <= 0 {
		return fmt.Errorf("buy/sell size cannot be zero or negative")
	}
	if c.buyPrice <= 0 || c.sellPrice <= 0 {
		return fmt.Errorf("buy/sell prices cannot be zero or negative")
	}
	if c.buyCancelOffset <= 0 || c.sellCancelOffset <= 0 {
		return fmt.Errorf("buy/sell cancel prices cannot be zero or negative")
	}
	if c.sellPrice-c.sellCancelOffset <= 0 {
		return fmt.Errorf("sell cancel price point cannot be zero or negative")
	}
	if c.buySize < c.sellSize {
		return fmt.Errorf("buy size cannot be lower than sell size")
	}
	if c.sellPrice <= c.buyPrice {
		return fmt.Errorf("sell price point must be above the buy price point")
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

	req := &api.LoopRequest{
		Product: c.product,
		Buy: point.Point{
			Size:   decimal.NewFromFloat(c.buySize),
			Price:  decimal.NewFromFloat(c.buyPrice),
			Cancel: decimal.NewFromFloat(c.buyPrice + c.buyCancelOffset),
		},
		Sell: point.Point{
			Size:   decimal.NewFromFloat(c.sellSize),
			Price:  decimal.NewFromFloat(c.sellPrice),
			Cancel: decimal.NewFromFloat(c.sellPrice - c.sellCancelOffset),
		},
	}
	resp, err := subcmds.Post[api.LoopResponse](ctx, &c.ClientFlags, api.LoopPath, req)
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
	fset.StringVar(&c.product, "product", "BCH-USD", "product id for the trade")
	fset.Float64Var(&c.buySize, "buy-size", 0, "buy-size for the trade")
	fset.Float64Var(&c.buyPrice, "buy-price", 0, "limit buy-price for the trade")
	fset.Float64Var(&c.buyCancelOffset, "buy-cancel-offset", 0, "buy-cancel price offset for the trade")
	fset.Float64Var(&c.sellSize, "sell-size", 0, "sell-size for the trade")
	fset.Float64Var(&c.sellPrice, "sell-price", 0, "limit sell-price for the trade")
	fset.Float64Var(&c.sellCancelOffset, "sell-cancel-offset", 0, "sell-cancel price offset for the trade")
	return fset, cli.CmdFunc(c.Run)
}

func (c *Add) Synopsis() string {
	return "Creates a new buy-sell loop job"
}

func (c *Add) CommandHelp() string {
	return `

Command "add" creates a limit-buy-then-limit-sell loop. Trading begins with a
limit-buy order and after it is executed successfully, a limit-sell order is
created. Price point for the limit-sell must be above the limit-buy price point
so that a positive profit can be secured. Asset size for the sell orders can be
lower than the buy-size, but it cannot be greater than the buy-size.

`
}
