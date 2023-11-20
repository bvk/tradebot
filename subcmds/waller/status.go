// Copyright (c) 2023 BVK Chaitanya

package waller

import (
	"context"
	"flag"
	"fmt"
	"time"

	"github.com/bvk/tradebot/cli"
	"github.com/bvk/tradebot/subcmds/db"
	"github.com/bvk/tradebot/waller"
	"github.com/bvkgo/kv"
	"github.com/shopspring/decimal"
)

type Status struct {
	db.Flags

	showBuys  bool
	showSells bool
	showPairs bool
}

func (c *Status) Run(ctx context.Context, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("this command takes one (waller-job-id) argument")
	}
	uid, err := c.Flags.GetJobID(ctx, args[0])
	if err != nil {
		return fmt.Errorf("could not resolve argument: %w", err)
	}

	var status *waller.Status
	getter := func(ctx context.Context, r kv.Reader) error {
		w, err := waller.Load(ctx, uid, r)
		if err != nil {
			return err
		}
		status = w.Status()
		return nil
	}

	db, err := c.Flags.GetDatabase(ctx)
	if err != nil {
		return err
	}
	if err := kv.WithReader(ctx, db, getter); err != nil {
		return err
	}

	var monthsPerYear = decimal.NewFromInt(12)

	// Print data for the waller.

	fmt.Printf("Budget: %s (effective fee at %.2f%%)\n", status.Budget().StringFixed(3), status.EffectiveFeePct())
	analysis := status.Analysis()
	for _, rate := range aprs {
		perYear := analysis.ProfitGoalForReturnRate(rate)
		fmt.Printf("%.1f%% Monthly Profit Goal: %s\n", rate, perYear.Div(monthsPerYear).StringFixed(3))
	}
	fmt.Println()
	fmt.Printf("Profit: %s\n", status.Profit().StringFixed(3))
	fmt.Printf("Num Days: %d days\n", status.Uptime()/(24*time.Hour))
	fmt.Printf("Return rate per year (projection): %s%%\n", status.ReturnRate().StringFixed(3))
	fmt.Printf("Return rate per month (projection): %s%%\n", status.ReturnRate().Div(monthsPerYear).StringFixed(3))
	fmt.Println()
	fmt.Printf("Num Buys: %d\n", status.NumBuys())
	fmt.Printf("Num Sells: %d\n", status.NumSells())
	fmt.Printf("Total Fees: %s (%.2f%%)\n", status.TotalFees().StringFixed(3), status.EffectiveFeePct())
	fmt.Println()
	fmt.Printf("Unsold Size: %s\n", status.UnsoldSize().StringFixed(3))
	fmt.Printf("Unsold Fees: %s\n", status.UnsoldFees().StringFixed(3))
	fmt.Printf("Unsold Value: %s\n", status.UnsoldValue().StringFixed(3))

	pairs := status.Pairs()
	if c.showPairs {
		fmt.Println()
		fmt.Println("Pairs")
		for i := range pairs {
			fmt.Printf("  %s: nbuys %d nsells %d (hold %s lockin %s) fees %s feePct %s%% profit %s\n", pairs[i], status.NumBuysForPair(i), status.NumSellsForPair(i), status.UnsoldSizeForPair(i).StringFixed(3), status.UnsoldValueForPair(i).StringFixed(3), status.FeesForPair(i).StringFixed(3), status.FeePctForPair(i).StringFixed(3), status.ProfitForPair(i).StringFixed(3))
		}
	}

	if c.showBuys {
		fmt.Println()
		fmt.Println("Buys")
		for i := range pairs {
			fmt.Printf("  %s: norders %d size %s feePct %s%% fees %s value %s\n", pairs[i].Buy,
				status.NumOrdersAtBuyPoint(i), status.TotalSizeAtBuyPoint(i).StringFixed(3), status.EffectiveFeePctAtBuyPoint(i).StringFixed(3), status.TotalFeesAtBuyPoint(i).StringFixed(3), status.TotalValueAtBuyPoint(i).StringFixed(3))
		}
	}

	if c.showSells {
		fmt.Println()
		fmt.Println("Sells")
		for i := range pairs {
			fmt.Printf("  %s: norders %d size %s feePct %s%% fees %s value %s\n", pairs[i].Sell,
				status.NumOrdersAtSellPoint(i), status.TotalSizeAtSellPoint(i).StringFixed(3), status.EffectiveFeePctAtSellPoint(i).StringFixed(3), status.TotalFeesAtSellPoint(i).StringFixed(3), status.TotalValueAtSellPoint(i).StringFixed(3))
		}
	}

	return nil
}

func (c *Status) Command() (*flag.FlagSet, cli.CmdFunc) {
	fset := flag.NewFlagSet("status", flag.ContinueOnError)
	c.Flags.SetFlags(fset)
	fset.BoolVar(&c.showPairs, "show-pairs", true, "when true, prints data for buy/sell loops with activity")
	fset.BoolVar(&c.showBuys, "show-buys", false, "when true, prints data for buy points with activity")
	fset.BoolVar(&c.showSells, "show-sells", false, "when true, prints data for sell points with activity")
	return fset, cli.CmdFunc(c.Run)
}

func (c *Status) Synopsis() string {
	return "Prints a waller trade's information"
}
