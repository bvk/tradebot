// Copyright (c) 2023 BVK Chaitanya

package waller

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/bvk/tradebot/namer"
	"github.com/bvk/tradebot/server"
	"github.com/bvk/tradebot/subcmds/cmdutil"
	"github.com/bvk/tradebot/waller"
	"github.com/bvkgo/kv"
	"github.com/visvasity/cli"
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
	s := wall.Status(nil)
	fmt.Println("UID", s.UID)
	fmt.Println("ProductID", s.ProductID)
	fmt.Println("ExchangeName", s.ExchangeName)
	fmt.Println()
	fmt.Println("Budget", s.Budget.StringFixed(5))
	fmt.Println("ReturnRate", s.ReturnRate().StringFixed(5))
	fmt.Println("AnnualReturnRate", s.AnnualReturnRate().StringFixed(5))
	fmt.Println("ProfitPerDay", s.ProfitPerDay().StringFixed(5))
	fmt.Println()
	fmt.Println("NumDays", s.NumDays())
	fmt.Println("NumBuys", s.NumBuys)
	fmt.Println("NumSells", s.NumSells)
	fmt.Println()
	fmt.Println("BoughtFees", s.BoughtFees.StringFixed(5))
	fmt.Println("BoughtSize", s.BoughtSize.StringFixed(5))
	fmt.Println("BoughtValue", s.BoughtValue.StringFixed(5))
	fmt.Println()
	fmt.Println("SoldFees", s.SoldFees.StringFixed(5))
	fmt.Println("SoldSize", s.SoldSize.StringFixed(5))
	fmt.Println("SoldValue", s.SoldValue.StringFixed(5))
	fmt.Println()
	fmt.Println("UnsoldFees", s.UnsoldFees.StringFixed(5))
	fmt.Println("UnsoldSize", s.UnsoldSize.StringFixed(5))
	fmt.Println("UnsoldValue", s.UnsoldValue.StringFixed(5))
	fmt.Println()
	fmt.Println("OversoldFees", s.OversoldFees.StringFixed(5))
	fmt.Println("OversoldSize", s.OversoldSize.StringFixed(5))
	fmt.Println("OversoldValue", s.OversoldValue.StringFixed(5))

	fmt.Println()
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 1, ' ', tabwriter.AlignRight)
	fmt.Fprintf(tw, "Pair\tBudget\tReturn\tAnnualReturn\tDays\tBuys\tSells\tProfit\tFees\tBoughtValue\tSoldValue\tUnsoldValue\tSoldSize\tUnsoldSize\t\n")
	for _, p := range wall.Pairs() {
		s := wall.PairStatus(p, nil)
		if c.skipZeroBuys && s.NumBuys == 0 {
			continue
		}
		id := fmt.Sprintf("%s-%s", p.Buy.Price.StringFixed(5), p.Sell.Price.StringFixed(5))
		fmt.Fprintf(tw, "%s\t%s\t%s%%\t%s%%\t%s\t%d\t%d\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t\n",
			id,
			s.Budget.StringFixed(0),
			s.ReturnRate().StringFixed(0),
			s.AnnualReturnRate().StringFixed(0),
			s.NumDays().StringFixed(1),
			s.NumBuys,
			s.NumSells,
			s.Profit().StringFixed(0),
			s.Fees().StringFixed(0),
			s.Bought().StringFixed(0),
			s.Sold().StringFixed(0),
			s.UnsoldValue.StringFixed(0),
			s.SoldSize.Sub(s.OversoldSize).StringFixed(5),
			s.UnsoldSize.StringFixed(5))
	}
	tw.Flush()
	return nil
}

func (c *Get) Command() (string, *flag.FlagSet, cli.CmdFunc) {
	fset := flag.NewFlagSet("get", flag.ContinueOnError)
	c.DBFlags.SetFlags(fset)
	fset.BoolVar(&c.skipZeroBuys, "skip-zero-buys", true, "when true, doesn't print inactive pairs")
	return "get", fset, cli.CmdFunc(c.Run)
}

func (c *Get) Purpose() string {
	return "Prints a waller state"
}
