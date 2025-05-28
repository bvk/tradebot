// Copyright (c) 2023 BVK Chaitanya

package exchange

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"

	"github.com/bvk/tradebot/api"
	"github.com/bvk/tradebot/subcmds/cmdutil"
	"github.com/visvasity/cli"
)

type GetOrder struct {
	cmdutil.ClientFlags

	name string

	product string
}

func (c *GetOrder) Command() (string, *flag.FlagSet, cli.CmdFunc) {
	fset := flag.NewFlagSet("get-order", flag.ContinueOnError)
	c.ClientFlags.SetFlags(fset)
	fset.StringVar(&c.name, "name", "coinbase", "name of the exchange")
	fset.StringVar(&c.product, "product", "", "name of the exchange product")
	return "get-order", fset, cli.CmdFunc(c.run)
}

func (c *GetOrder) Purpose() string {
	return "Fetches an order metadata from the local data store."
}

func (c *GetOrder) run(ctx context.Context, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("this command takes one (order-id) argument")
	}

	req := &api.ExchangeGetOrderRequest{
		ExchangeName: c.name,
		ProductID:    c.product,
		OrderID:      args[0],
	}
	if err := req.Check(); err != nil {
		return err
	}

	resp, err := cmdutil.Post[api.ExchangeGetOrderResponse](ctx, &c.ClientFlags, api.ExchangeGetOrderPath, req)
	if err != nil {
		return fmt.Errorf("POST request to get-order failed: %w", err)
	}
	jsdata, _ := json.MarshalIndent(resp, "", "  ")
	fmt.Printf("%s\n", jsdata)
	return nil
}
