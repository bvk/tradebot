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
	"time"

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

	startTime string
}

func (c *Status) Synopsis() string {
	return "Status prints global summary of all trade jobs"
}

func (c *Status) Command() (*flag.FlagSet, cli.CmdFunc) {
	fset := flag.NewFlagSet("status", flag.ContinueOnError)
	c.DBFlags.SetFlags(fset)
	fset.StringVar(&c.startTime, "start-time", "2023-11-01T00:00:00Z", "Time when tradebot was deployed to production")
	return fset, cli.CmdFunc(c.run)
}

func (c *Status) run(ctx context.Context, args []string) error {
	start, err := time.Parse(time.RFC3339, c.startTime)
	if err != nil {
		return err
	}
	ndays := int(time.Now().Sub(start) / (24 * time.Hour))

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
	jobs = slices.DeleteFunc(jobs, func(j trader.Job) bool {
		_, ok := j.(*limiter.Limiter)
		return ok
	})
	sort.Slice(jobs, func(i, j int) bool {
		return jobs[i].ProductID() < jobs[j].ProductID()
	})

	var fees, bought, sold, unsold decimal.Decimal
	for _, j := range jobs {
		fees = fees.Add(j.Fees())
		bought = bought.Add(j.BoughtValue())
		sold = sold.Add(j.SoldValue())
		unsold = unsold.Add(j.UnsoldValue())
	}

	var (
		d30  = decimal.NewFromInt(30)
		d100 = decimal.NewFromInt(100)
		d365 = decimal.NewFromInt(365)
	)

	fmt.Printf("Num Days: %d\n", ndays)
	fmt.Println()
	fmt.Printf("Fees: %s\n", fees.StringFixed(3))
	fmt.Printf("Sold: %s\n", sold.StringFixed(3))
	fmt.Printf("Bought: %s\n", bought.StringFixed(3))
	fmt.Printf("Unsold: %s\n", unsold.StringFixed(3))

	feePct := fees.Mul(d100).Div(sold.Add(bought))
	profit := sold.Sub(bought.Sub(unsold)).Sub(fees)
	profitPerDay := profit.Div(decimal.NewFromInt(int64(ndays)))

	fmt.Println()
	fmt.Printf("Profit: %s\n", profit.StringFixed(3))
	fmt.Printf("Effective Fee Pct: %s%%\n", feePct.StringFixed(3))

	fmt.Println()
	fmt.Printf("Profit per month: %s\n", profitPerDay.Mul(d30).StringFixed(3))
	fmt.Printf("Profit per year: %s\n", profitPerDay.Mul(d365).StringFixed(3))

	fmt.Println()
	rates := []float64{2.625, 5, 8, 10, 15, 20}
	for _, rate := range rates {
		covered := profit.Div(decimal.NewFromFloat(rate).Div(d100))
		fmt.Printf("Investment covered at %.03f APY: %s\n", rate, covered.StringFixed(3))
	}

	fmt.Println()
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 1, ' ', tabwriter.AlignRight)
	fmt.Fprintf(tw, "UID\tProduct\tProfit\tFees\tBought\tSold\tUnsold\t\n")
	for _, job := range jobs {
		uid := job.UID()
		pid := job.ProductID()
		fees := job.Fees()
		bought := job.BoughtValue()
		sold := job.SoldValue()
		unsold := job.UnsoldValue()
		profit := sold.Sub(bought.Sub(unsold)).Sub(fees)
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t\n", uid, pid, profit.StringFixed(3), fees.StringFixed(3), bought.StringFixed(3), sold.StringFixed(3), unsold.StringFixed(3))
	}
	tw.Flush()
	return nil
}
