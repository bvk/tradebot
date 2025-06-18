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

type GetPrices struct {
}

func (c *GetPrices) Purpose() string {
	return "Prints Crypto prices from CoinEx markets."
}

func (c *GetPrices) Command() (string, *flag.FlagSet, cli.CmdFunc) {
	fset := flag.NewFlagSet("get-prices", flag.ContinueOnError)
	return "get-prices", fset, cli.CmdFunc(c.run)
}

func (c *GetPrices) run(ctx context.Context, args []string) error {
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
