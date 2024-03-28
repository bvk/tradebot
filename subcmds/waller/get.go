// Copyright (c) 2023 BVK Chaitanya

package waller

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/bvk/tradebot/cli"
	"github.com/bvk/tradebot/namer"
	"github.com/bvk/tradebot/server"
	"github.com/bvk/tradebot/subcmds/cmdutil"
	"github.com/bvk/tradebot/waller"
	"github.com/bvkgo/kv"
)

type Get struct {
	cmdutil.DBFlags

	skipZeroBuys bool
}

func (c *Get) Run(ctx context.Context, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("this command takes one waller argument")
	}
	arg := args[0]

	var wall *waller.Waller
	getter := func(ctx context.Context, r kv.Reader) error {
		_, uid, _, err := namer.Resolve(ctx, r, arg)
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("could not resolve waller argument %q: %w", arg, err)
			}
			uid = arg
		}

		job, err := server.Load(ctx, r, uid, "waller")
		if err != nil {
			return fmt.Errorf("could not load waller from db: %w", err)
		}
		wall = job.(*waller.Waller)
		return nil
	}

	db, closer, err := c.DBFlags.GetDatabase(ctx)
	if err != nil {
		return err
	}
	defer closer()

	if err := kv.WithReader(ctx, db, getter); err != nil {
		return err
	}

	if wall == nil {
		return fmt.Errorf("could not load waller (unexpected)")
	}

	// Print the waller state in a human readable format.
	s := wall.Status()
	fmt.Println("UID", s.UID)
	fmt.Println("ProductID", s.ProductID)
	fmt.Println("ExchangeName", s.ExchangeName)
	fmt.Println()
	fmt.Println("Budget", s.Budget.StringFixed(3))
	fmt.Println("ReturnRate", s.ReturnRate().StringFixed(3))
	fmt.Println("AnnualReturnRate", s.AnnualReturnRate().StringFixed(3))
	fmt.Println("ProfitPerDay", s.ProfitPerDay().StringFixed(3))
	fmt.Println()
	fmt.Println("NumDays", s.NumDays())
	fmt.Println("NumBuys", s.NumBuys)
	fmt.Println("NumSells", s.NumSells)
	fmt.Println()
	fmt.Println("BoughtFees", s.BoughtFees.StringFixed(3))
	fmt.Println("BoughtSize", s.BoughtSize.StringFixed(3))
	fmt.Println("BoughtValue", s.BoughtValue.StringFixed(3))
	fmt.Println()
	fmt.Println("SoldFees", s.SoldFees.StringFixed(3))
	fmt.Println("SoldSize", s.SoldSize.StringFixed(3))
	fmt.Println("SoldValue", s.SoldValue.StringFixed(3))
	fmt.Println()
	fmt.Println("UnsoldFees", s.UnsoldFees.StringFixed(3))
	fmt.Println("UnsoldSize", s.UnsoldSize.StringFixed(3))
	fmt.Println("UnsoldValue", s.UnsoldValue.StringFixed(3))
	fmt.Println()
	fmt.Println("OversoldFees", s.OversoldFees.StringFixed(3))
	fmt.Println("OversoldSize", s.OversoldSize.StringFixed(3))
	fmt.Println("OversoldValue", s.OversoldValue.StringFixed(3))

	fmt.Println()
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 1, ' ', tabwriter.AlignRight)
	fmt.Fprintf(tw, "Pair\tBudget\tReturn\tAnnualReturn\tDays\tBuys\tSells\tProfit\tFees\tBoughtValue\tSoldValue\tUnsoldValue\tSoldSize\tUnsoldSize\t\n")
	for _, p := range wall.Pairs() {
		s := wall.PairStatus(p)
		if c.skipZeroBuys && s.NumBuys == 0 {
			continue
		}
		id := fmt.Sprintf("%s-%s", p.Buy.Price.StringFixed(2), p.Sell.Price.StringFixed(2))
		fmt.Fprintf(tw, "%s\t%s\t%s%%\t%s%%\t%s\t%d\t%d\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t\n",
			id,
			s.Budget.StringFixed(3),
			s.ReturnRate().StringFixed(3),
			s.AnnualReturnRate().StringFixed(3),
			s.NumDays().StringFixed(3),
			s.NumBuys,
			s.NumSells,
			s.Profit().StringFixed(3),
			s.Fees().StringFixed(3),
			s.Bought().StringFixed(3),
			s.Sold().StringFixed(3),
			s.UnsoldValue.StringFixed(3),
			s.SoldSize.Sub(s.OversoldSize).StringFixed(3),
			s.UnsoldSize.StringFixed(3))
	}
	tw.Flush()
	return nil
}

func (c *Get) Command() (*flag.FlagSet, cli.CmdFunc) {
	fset := flag.NewFlagSet("get", flag.ContinueOnError)
	c.DBFlags.SetFlags(fset)
	fset.BoolVar(&c.skipZeroBuys, "skip-zero-buys", true, "when true, doesn't print inactive pairs")
	return fset, cli.CmdFunc(c.Run)
}

func (c *Get) Synopsis() string {
	return "Prints a waller state"
}
