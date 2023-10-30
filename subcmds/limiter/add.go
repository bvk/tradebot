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

	fset *flag.FlagSet

	product string

	size        float64
	price       float64
	cancelPrice float64
}

func (c *Add) Run(ctx context.Context, args []string) error {
	if len(args) != 0 {
		return fmt.Errorf("this command takes no arguments")
	}
	if c.size <= 0 {
		return fmt.Errorf("size cannot be zero or negative")
	}
	if c.price <= 0 {
		return fmt.Errorf("price cannot be zero or negative")
	}
	if c.cancelPrice <= 0 {
		return fmt.Errorf("cancel-price cannot be zero or negative")
	}

	req := &api.LimitRequest{
		Product:     c.product,
		Size:        decimal.NewFromFloat(c.size),
		Price:       decimal.NewFromFloat(c.price),
		CancelPrice: decimal.NewFromFloat(c.cancelPrice),
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
	if c.fset == nil {
		c.fset = flag.NewFlagSet("add", flag.ContinueOnError)
		c.ClientFlags.SetFlags(c.fset)
		c.fset.Float64Var(&c.size, "size", 0, "asset size for the trade")
		c.fset.Float64Var(&c.price, "price", 0, "limit price for the trade")
		c.fset.Float64Var(&c.cancelPrice, "cancel-price", 0, "cancel-at price for the trade")
		c.fset.StringVar(&c.product, "product", "BCH-USD", "product id for the trade")
	}
	return c.fset, cli.CmdFunc(c.Run)
}
