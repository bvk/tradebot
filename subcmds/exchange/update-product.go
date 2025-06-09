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

type UpdateProduct struct {
	cmdutil.ClientFlags

	exchangeName string
	productType  string

	enable bool
}

func (c *UpdateProduct) Command() (string, *flag.FlagSet, cli.CmdFunc) {
	fset := flag.NewFlagSet("get-product", flag.ContinueOnError)
	c.ClientFlags.SetFlags(fset)
	fset.StringVar(&c.exchangeName, "exchange", "", "name of the exchange")
	fset.StringVar(&c.productType, "product-type", "SPOT", "type of the exchange product")
	fset.BoolVar(&c.enable, "enable", false, "enable/disable the product")
	return "update-product", fset, cli.CmdFunc(c.run)
}

func (c *UpdateProduct) run(ctx context.Context, args []string) error {
	if len(args) != 2 {
		return fmt.Errorf("this command needs base and quote currency name arguments")
	}

	req := &api.ExchangeUpdateProductRequest{
		ExchangeName: c.exchangeName,
		ProductType:  c.productType,
		Base:         args[0],
		Quote:        args[1],
		Enable:       c.enable,
	}
	if err := req.Check(); err != nil {
		return err
	}
	resp, err := cmdutil.Post[api.ExchangeUpdateProductResponse](ctx, &c.ClientFlags, api.ExchangeUpdateProductPath, req)
	if err != nil {
		return fmt.Errorf("POST request to get-product failed: %w", err)
	}

	jsdata, _ := json.MarshalIndent(resp, "", "  ")
	fmt.Printf("%s\n", jsdata)
	return nil
}
