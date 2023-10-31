// Copyright (c) 2023 BVK Chaitanya

package limiter

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"

	"github.com/bvkgo/tradebot/api"
	"github.com/bvkgo/tradebot/cli"
	"github.com/bvkgo/tradebot/subcmds"
	"github.com/shopspring/decimal"
)

type Add struct {
	subcmds.ClientFlags

	product string

	side         string
	size         float64
	price        float64
	cancelOffset float64
}

func (c *Add) check() error {
	if c.size <= 0 {
		return fmt.Errorf("size cannot be zero or negative")
	}
	if c.price <= 0 {
		return fmt.Errorf("price cannot be zero or negative")
	}
	if c.cancelOffset <= 0 {
		return fmt.Errorf("cancel-offset cannot be zero or negative")
	}
	if c.side != "BUY" && c.side != "SELL" {
		return fmt.Errorf("side must be one of BUY or SELL")
	}

	var cancelPrice float64
	if c.side == "BUY" {
		cancelPrice = c.price + c.cancelOffset
	} else {
		cancelPrice = c.price - c.cancelOffset
	}
	if cancelPrice <= 0 {
		return fmt.Errorf("cancel-price cannot be zero or negative")
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

	var cancelPrice float64
	if c.side == "BUY" {
		cancelPrice = c.price + c.cancelOffset
	} else {
		cancelPrice = c.price - c.cancelOffset
	}

	req := &api.LimitRequest{
		Product:     c.product,
		Size:        decimal.NewFromFloat(c.size),
		Price:       decimal.NewFromFloat(c.price),
		CancelPrice: decimal.NewFromFloat(cancelPrice),
	}
	resp, err := subcmds.Post[api.LimitResponse](ctx, &c.ClientFlags, "/trader/limit", req)
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
	fset.Float64Var(&c.size, "size", 0, "asset size for the trade")
	fset.Float64Var(&c.price, "price", 0, "limit price for the trade")
	fset.StringVar(&c.side, "side", "", "must be one of BUY or SELL")
	fset.Float64Var(&c.cancelOffset, "cancel-offset", 0, "cancel-price offset for the trade")
	fset.StringVar(&c.product, "product", "BCH-USD", "product id for the trade")
	return fset, cli.CmdFunc(c.Run)
}

func (c *Add) Synopsis() string {
	return "Creates a new limit buy/sell job"
}

func (c *Add) CommandHelp() string {
	return `

Command "add" creates a new limit-buy or limit-sell job with an automatic
cancellation price point. Exchange orders are canceled when the ticker crosses
the cancel price and recreated when the ticker comes close to the limit
price. This automatic cancellation unlocks funds from the exchange so that they
are available for other jobs.

Note that, cancellation price for buy and sell orders will be above and below
the limit-price respectively. Buy orders are canceled when the ticker price
moves above the cancel-price and sell orders are canceled when the ticker price
moves below the cancel-price.

`
}
