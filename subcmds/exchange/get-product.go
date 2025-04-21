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

type GetProduct struct {
	cmdutil.ClientFlags

	name string
}

func (c *GetProduct) Command() (string, *flag.FlagSet, cli.CmdFunc) {
	fset := flag.NewFlagSet("get-product", flag.ContinueOnError)
	c.ClientFlags.SetFlags(fset)
	fset.StringVar(&c.name, "name", "coinbase", "name of the exchange")
	return "get-product", fset, cli.CmdFunc(c.run)
}

func (c *GetProduct) run(ctx context.Context, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("this command takes one (product-id) argument")
	}

	req := &api.ExchangeGetProductRequest{
		ExchangeName: c.name,
		ProductID:    args[0],
	}
	resp, err := cmdutil.Post[api.ExchangeGetProductResponse](ctx, &c.ClientFlags, api.ExchangeGetProductPath, req)
	if err != nil {
		return fmt.Errorf("POST request to get-product failed: %w", err)
	}

	jsdata, _ := json.MarshalIndent(resp, "", "  ")
	fmt.Printf("%s\n", jsdata)
	return nil
}
