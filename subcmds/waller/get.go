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
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 1, ' ', tabwriter.AlignRight)
	fmt.Fprintf(tw, "Pair\tBudget\tReturn\tAnnualReturn\tDays\tBuys\tSells\tProfit\tFees\tBoughtValue\tSoldValue\tUnsoldValue\tSoldSize\tUnsoldSize\t\n")
	for _, p := range wall.Pairs() {
		s := wall.PairStatus(p)
		id := fmt.Sprintf("%s-%s", p.Buy.Price.StringFixed(2), p.Sell.Price.StringFixed(2))
		fmt.Fprintf(tw, "%s\t%s\t%s%%\t%s%%\t%d\t%d\t%d\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t\n",
			id,
			s.Budget.StringFixed(3),
			s.ReturnRate().StringFixed(3),
			s.AnnualReturnRate().StringFixed(3),
			s.NumDays(),
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
	return fset, cli.CmdFunc(c.Run)
}

func (c *Get) Synopsis() string {
	return "Prints a waller state"
}
