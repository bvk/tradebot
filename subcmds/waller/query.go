// Copyright (c) 2023 BVK Chaitanya

package waller

import (
	"context"
	"flag"
	"fmt"

	"github.com/bvk/tradebot/cli"
	"github.com/shopspring/decimal"
)

var aprs = []float64{5, 10, 20, 30}

type Query struct {
	spec Spec

	lockinPrice float64
}

func (c *Query) run(ctx context.Context, args []string) error {
	if err := c.spec.check(); err != nil {
		return err
	}
	pairs := c.spec.BuySellPairs()

	var lockinPrice decimal.Decimal
	if c.lockinPrice != 0 {
		lockinPrice = decimal.NewFromFloat(c.lockinPrice)
	} else {
		lockinPrice = pairs[0].Buy.Price.Add(pairs[len(pairs)-1].Buy.Price).Div(Two)
	}

	fmt.Printf("Budget required: %s\n", c.spec.Budget().StringFixed(2))
	fmt.Printf("Fee percentage: %.2f%%\n", c.spec.feePercentage)

	fmt.Println()
	fmt.Printf("Num Buy/Sell pairs: %d\n", len(pairs))
	fmt.Printf("Max lockin at price %s: %s\n", lockinPrice.StringFixed(2), c.spec.LockinAmountAt(lockinPrice).StringFixed(2))

	fmt.Println()
	fmt.Printf("Minimum loop fee: %s\n", c.spec.MinLoopFee().StringFixed(2))
	fmt.Printf("Minimum price margin: %s\n", c.spec.MinPriceMargin().StringFixed(2))
	fmt.Printf("Minimum profit margin: %s\n", c.spec.MinProfitMargin().StringFixed(2))

	fmt.Println()
	fmt.Printf("Maximum loop fee: %s\n", c.spec.MaxLoopFee().StringFixed(2))
	fmt.Printf("Maximum price margin: %s\n", c.spec.MaxPriceMargin().StringFixed(2))
	fmt.Printf("Maximum profit margin: %s\n", c.spec.MaxProfitMargin().StringFixed(2))

	nsells := []int{1, 5, 10, 20, 25, 30, 40, 50, 60, 70, 75, 80, 90, 100}
	fmt.Println()
	for _, nsell := range nsells {
		perYear := c.spec.AvgProfitMargin().Mul(decimal.NewFromInt(int64(nsell)))
		rate := perYear.Div(c.spec.Budget()).Mul(decimal.NewFromInt(100))
		fmt.Printf("Return rate for %d sells per year: %s%%\n", nsell, rate.StringFixed(3))
	}
	fmt.Println()
	for _, nsell := range nsells {
		perYear := c.spec.AvgProfitMargin().Mul(decimal.NewFromInt(int64(nsell * 12)))
		rate := perYear.Div(c.spec.Budget()).Mul(decimal.NewFromInt(100))
		fmt.Printf("Return rate for %d sells per month: %s%%\n", nsell, rate.StringFixed(3))
	}

	fmt.Println()
	for _, rate := range aprs {
		nsells := c.spec.NumSellsPerYear(rate)
		fmt.Printf("For %.1f%% return\n", rate)
		fmt.Println()
		fmt.Printf("  Num sells per year:  %.2f\n", float64(nsells))
		fmt.Printf("  Num sells per month:  %.2f\n", float64(nsells)/12.0)
		fmt.Printf("  Num sells per day:  %.2f\n", float64(nsells)/365.0)
		fmt.Println()
	}

	return nil
}

func (c *Query) Command() (*flag.FlagSet, cli.CmdFunc) {
	fset := flag.NewFlagSet("query", flag.ContinueOnError)
	c.spec.SetFlags(fset)
	fset.Float64Var(&c.lockinPrice, "lockin-price", 0, "ticker price for the expected lockin amount")
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
