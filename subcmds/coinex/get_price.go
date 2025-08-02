// Copyright (c) 2025 BVK Chaitanya

package coinex

import (
	"context"
	"flag"
	"fmt"
	"strings"

	"github.com/bvk/tradebot/coinex"
	"github.com/visvasity/cli"
)

type GetPrice struct {
}

func (c *GetPrice) Purpose() string {
	return "Prints Crypto price from CoinEx markets."
}

func (c *GetPrice) Command() (string, *flag.FlagSet, cli.CmdFunc) {
	fset := flag.NewFlagSet("get-price", flag.ContinueOnError)
	return "get-price", fset, cli.CmdFunc(c.run)
}

func (c *GetPrice) run(ctx context.Context, args []string) error {
	priceMap, err := coinex.GetPriceMap(ctx)
	if err != nil {
		return err
	}

	if len(args) == 0 {
		for k, v := range priceMap {
			fmt.Printf("%s: %s\n", k, v)
		}
		return nil
	}

	for _, arg := range args {
		arg = strings.ToUpper(arg)
		price, ok := priceMap[arg]
		if !ok {
			fmt.Printf("%s:\n", arg)
		} else {
			fmt.Printf("%s: %s\n", arg, price)
		}
	}
	return nil
}
