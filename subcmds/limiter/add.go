// Copyright (c) 2023 BVK Chaitanya

package limiter

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"

	"github.com/bvk/tradebot/api"
	"github.com/bvk/tradebot/point"
	"github.com/bvk/tradebot/subcmds/cmdutil"
	"github.com/shopspring/decimal"
	"github.com/visvasity/cli"
)

type Add struct {
	cmdutil.ClientFlags

	product  string
	exchange string

	side         string
	size         float64
	price        float64
	cancelOffset float64
}

func (c *Add) check() error {
	if len(c.product) == 0 {
		return fmt.Errorf("product name cannot be empty")
	}
	if len(c.exchange) == 0 {
		return fmt.Errorf("exchange name cannot be empty")
	}

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
		ProductID:    c.product,
		ExchangeName: c.exchange,
		Point: &point.Point{
			Size:   decimal.NewFromFloat(c.size),
			Price:  decimal.NewFromFloat(c.price),
			Cancel: decimal.NewFromFloat(cancelPrice),
		},
	}
	resp, err := cmdutil.Post[api.LimitResponse](ctx, &c.ClientFlags, api.LimitPath, req)
	if err != nil {
		return err
	}
	jsdata, _ := json.MarshalIndent(resp, "", "  ")
	fmt.Printf("%s\n", jsdata)
	return nil
}

func (c *Add) Command() (string, *flag.FlagSet, cli.CmdFunc) {
	fset := flag.NewFlagSet("add", flag.ContinueOnError)
	c.ClientFlags.SetFlags(fset)
	fset.Float64Var(&c.size, "size", 0, "asset size for the trade")
	fset.Float64Var(&c.price, "price", 0, "limit price for the trade")
	fset.StringVar(&c.side, "side", "", "must be one of BUY or SELL")
	fset.Float64Var(&c.cancelOffset, "cancel-offset", 0, "cancel-price offset for the trade")
	fset.StringVar(&c.product, "product", "", "product id for the trade")
	fset.StringVar(&c.exchange, "exchange", "coinbase", "exchange name for the product")
	return "add", fset, cli.CmdFunc(c.Run)
}

func (c *Add) Purpose() string {
	return "Creates a new limit buy/sell job"
}

func (c *Add) Description() string {
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
