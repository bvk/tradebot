// Copyright (c) 2023 BVK Chaitanya

package waller

import (
	"context"
	"flag"

	"github.com/bvk/tradebot/cli"
	"github.com/bvk/tradebot/waller"
)

var aprs = []float64{5, 10, 20, 30}

type Query struct {
	spec Spec
}

func (c *Query) run(ctx context.Context, args []string) error {
	if err := c.spec.Check(); err != nil {
		return err
	}
	pairs := c.spec.BuySellPairs()
	feePct := c.spec.feePercentage
	a := waller.Analyze(pairs, feePct)
	PrintAnalysis(a)
	return nil
}

func (c *Query) Command() (*flag.FlagSet, cli.CmdFunc) {
	fset := flag.NewFlagSet("query", flag.ContinueOnError)
	c.spec.SetFlags(fset)
	return fset, cli.CmdFunc(c.run)
}

func (c *Query) Synopsis() string {
	return "Print summary for a job"
}

func (c *Query) CommandHelp() string {
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
