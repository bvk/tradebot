// Copyright (c) 2023 BVK Chaitanya

package subcmds

import (
	"context"
	"flag"
	"fmt"
	"os"
	"slices"
	"sort"
	"text/tabwriter"

	"github.com/bvk/tradebot/cli"
	"github.com/bvk/tradebot/limiter"
	"github.com/bvk/tradebot/server"
	"github.com/bvk/tradebot/subcmds/cmdutil"
	"github.com/bvk/tradebot/trader"
	"github.com/bvkgo/kv"
	"github.com/shopspring/decimal"
)

type Status struct {
	cmdutil.DBFlags
}

func (c *Status) Synopsis() string {
	return "Status prints global summary of all trade jobs"
}

func (c *Status) Command() (*flag.FlagSet, cli.CmdFunc) {
	fset := flag.NewFlagSet("status", flag.ContinueOnError)
	c.DBFlags.SetFlags(fset)
	return fset, cli.CmdFunc(c.run)
}

func (c *Status) run(ctx context.Context, args []string) error {
	db, err := c.DBFlags.GetDatabase(ctx)
	if err != nil {
		return err
	}

	var jobs []trader.Job
	load := func(ctx context.Context, r kv.Reader) error {
		vs, err := server.LoadTraders(ctx, r)
		if err != nil {
			return fmt.Errorf("could not load traders: %w", err)
		}
		jobs = vs
		return nil
	}
	if err := kv.WithReader(ctx, db, load); err != nil {
		return err
	}

	// Remove limiter jobs cause they are one-side trades.
	jobs = slices.DeleteFunc(jobs, func(j trader.Job) bool {
		_, ok := j.(*limiter.Limiter)
		return ok
	})
	sort.Slice(jobs, func(i, j int) bool {
		return jobs[i].ProductID() < jobs[j].ProductID()
	})

	var statuses []*trader.Status
	for _, j := range jobs {
		if s := trader.GetStatus(j); s != nil {
			statuses = append(statuses, s)
		}
	}

	var (
		d30  = decimal.NewFromInt(30)
		d100 = decimal.NewFromInt(100)
		d365 = decimal.NewFromInt(365)
	)

	sum := trader.Summarize(statuses)
	fmt.Printf("Num Days: %d\n", sum.NumDays)
	fmt.Println()
	fmt.Printf("Fees: %s\n", sum.TotalFees.StringFixed(3))
	fmt.Printf("Sold: %s\n", sum.SoldValue.StringFixed(3))
	fmt.Printf("Bought: %s\n", sum.BoughtValue.StringFixed(3))
	fmt.Printf("Unsold: %s\n", sum.UnsoldValue.StringFixed(3))

	fmt.Println()
	fmt.Printf("Profit: %s\n", sum.Profit().StringFixed(3))
	fmt.Printf("Effective Fee Pct: %s%%\n", sum.FeePct().StringFixed(3))

	fmt.Println()
	fmt.Printf("Profit per month: %s\n", sum.ProfitPerDay().Mul(d30).StringFixed(3))
	fmt.Printf("Profit per year: %s\n", sum.ProfitPerDay().Mul(d365).StringFixed(3))

	fmt.Println()
	rates := []float64{2.625, 5, 8, 10, 15, 20}
	for _, rate := range rates {
		covered := sum.Profit().Div(decimal.NewFromFloat(rate).Div(d100))
		fmt.Printf("Investment already covered at %.03f APY: %s\n", rate, covered.StringFixed(3))
	}
	fmt.Println()
	for _, rate := range rates {
		projected := sum.ProfitPerDay().Mul(d365).Div(decimal.NewFromFloat(rate).Div(d100))
		fmt.Printf("Projected investment covered at %.03f APY: %s\n", rate, projected.StringFixed(3))
	}

	fmt.Println()
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 1, ' ', tabwriter.AlignRight)
	fmt.Fprintf(tw, "UID\tProduct\tProfit\tFees\tBought\tSold\tUnsold\t\n")
	for _, s := range statuses {
		uid := s.UID()
		pid := s.ProductID()
		fees := s.TotalFees()
		bought := s.BoughtValue()
		sold := s.SoldValue()
		unsold := s.UnsoldValue()
		profit := s.Profit()
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t\n", uid, pid, profit.StringFixed(3), fees.StringFixed(3), bought.StringFixed(3), sold.StringFixed(3), unsold.StringFixed(3))
	}
	tw.Flush()
	return nil
}
