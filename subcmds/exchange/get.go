// Copyright (c) 2023 BVK Chaitanya

package exchange

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"

	"github.com/bvk/tradebot/api"
	"github.com/bvk/tradebot/cli"
	"github.com/bvk/tradebot/subcmds"
)

type Get struct {
	subcmds.ClientFlags

	name string
}

func (c *Get) Command() (*flag.FlagSet, cli.CmdFunc) {
	fset := flag.NewFlagSet("get", flag.ContinueOnError)
	c.ClientFlags.SetFlags(fset)
	fset.StringVar(&c.name, "name", "coinbase", "name of the exchange")
	return fset, cli.CmdFunc(c.run)
}

func (c *Get) run(ctx context.Context, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("this command takes one (order-id) argument")
	}

	req := &api.ExchangeGetRequest{
		Name:    c.name,
		OrderID: args[0],
	}
	resp, err := subcmds.Post[api.ExchangeGetResponse](ctx, &c.ClientFlags, "/exchange/get", req)
	if err != nil {
		return fmt.Errorf("POST request to exchange/get failed: %w", err)
	}
	jsdata, _ := json.MarshalIndent(resp, "", "  ")
	fmt.Printf("%s\n", jsdata)
	return nil
}
