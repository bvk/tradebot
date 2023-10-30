// Copyright (c) 2023 BVK Chaitanya

package looper

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

	fset *flag.FlagSet

	product string

	buySize        float64
	buyPrice       float64
	buyCancelPrice float64

	sellSize        float64
	sellPrice       float64
	sellCancelPrice float64
}

func (c *Add) check() error {
	if c.buySize <= 0 || c.sellSize <= 0 {
		return fmt.Errorf("buy/sell size cannot be zero or negative")
	}
	if c.buyPrice <= 0 || c.sellPrice <= 0 {
		return fmt.Errorf("buy/sell prices cannot be zero or negative")
	}
	if c.buyCancelPrice <= 0 || c.sellCancelPrice <= 0 {
		return fmt.Errorf("buy/sell cancel prices cannot be zero or negative")
	}
	if c.buySize < c.sellSize {
		return fmt.Errorf("buy size cannot be lower than sell size")
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
		Product:         c.product,
		BuySize:         decimal.NewFromFloat(c.buySize),
		BuyPrice:        decimal.NewFromFloat(c.buyPrice),
		BuyCancelPrice:  decimal.NewFromFloat(c.buyCancelPrice),
		SellSize:        decimal.NewFromFloat(c.sellSize),
		SellPrice:       decimal.NewFromFloat(c.sellPrice),
		SellCancelPrice: decimal.NewFromFloat(c.sellCancelPrice),
	}
	resp, err := subcmds.Post[api.LoopResponse](ctx, &c.ClientFlags, "/trader/loop", req)
	if err != nil {
		return err
	}
	jsdata, _ := json.MarshalIndent(resp, "", "  ")
	fmt.Printf("%s\n", jsdata)
	return nil
}

func (c *Add) Command() (*flag.FlagSet, cli.CmdFunc) {
	if c.fset == nil {
		c.fset = flag.NewFlagSet("add", flag.ContinueOnError)
		c.ClientFlags.SetFlags(c.fset)
		c.fset.StringVar(&c.product, "product", "BCH-USD", "product id for the trade")
		c.fset.Float64Var(&c.buySize, "buy-size", 0, "asset buy-size for the trade")
		c.fset.Float64Var(&c.buyPrice, "buy-price", 0, "asset limit buy-price for the trade")
		c.fset.Float64Var(&c.buyCancelPrice, "buy-cancel-price", 0, "asset buy-cancel-at price for the trade")
		c.fset.Float64Var(&c.sellSize, "sell-size", 0, "asset sell-size for the trade")
		c.fset.Float64Var(&c.sellPrice, "sell-price", 0, "asset limit sell-price for the trade")
		c.fset.Float64Var(&c.sellCancelPrice, "sell-cancel-price", 0, "asset sell-cancel-at price for the trade")
	}
	return c.fset, cli.CmdFunc(c.Run)
}
