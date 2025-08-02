// Copyright (c) 2023 BVK Chaitanya

package waller

import (
	"context"
	"flag"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/bvk/tradebot/waller"
	"github.com/shopspring/decimal"
	"github.com/visvasity/cli"
)

var aprs = []float64{5, 10, 20, 30}

type Query struct {
	spec Spec

	printPairs bool
}

func (c *Query) run(ctx context.Context, args []string) error {
	if err := c.spec.Check(); err != nil {
		return err
	}
	pairs := c.spec.BuySellPairs()
	feePct := c.spec.feePercentage
	a := waller.Analyze(pairs, decimal.NewFromFloat(feePct))
	PrintAnalysis(a)

	if c.printPairs {
		d100 := decimal.NewFromInt(100)
		feePctDec := decimal.NewFromFloat(feePct)
		tw := tabwriter.NewWriter(os.Stdout, 0, 0, 1, ' ', tabwriter.AlignRight)
		fmt.Fprintf(tw, "BuySize\tBuyPrice\tSellSize\tSellPrice\tPriceMargin\tProfit\t\n")
		for _, p := range pairs {
			bfee := p.Buy.Price.Mul(p.Buy.Size).Mul(feePctDec).Div(d100)
			sfee := p.Sell.Price.Mul(p.Sell.Size).Mul(feePctDec).Div(d100)
			profit := p.ValueMargin().Sub(bfee).Sub(sfee)
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t\n", p.Buy.Size.StringFixed(5), p.Buy.Price.StringFixed(5), p.Sell.Size.StringFixed(5), p.Sell.Price.StringFixed(5), p.Sell.Price.Sub(p.Buy.Price).StringFixed(5), profit.StringFixed(2))
		}
		tw.Flush()
	}
	return nil
}

func (c *Query) Command() (string, *flag.FlagSet, cli.CmdFunc) {
	fset := flag.NewFlagSet("query", flag.ContinueOnError)
	c.spec.SetFlags(fset)
	fset.BoolVar(&c.printPairs, "print-pairs", false, "when true, prints buy-sell points")
	return "query", fset, cli.CmdFunc(c.run)
}

func (c *Query) Purpose() string {
	return "Print summary for a waller job"
}

func (c *Query) Description() string {
	return `

Command "query" prints basic summary for a hypothetical waller job. This command
can be used to understand the required budget and "expected" profit returns for
a wall job without actually spending the funds in an exchange.

Users can get the following information for a waller job:

  - Total budget required for the job
  - Average fee for each buy-sell loop

  - Number of sells required per month for returns at 5%, 10%, etc.
  - TODO: Minimum volatility required for returns at 5%, 10%, etc.

`
}
