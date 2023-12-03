// Copyright (c) 2023 BVK Chaitanya

package subcmds

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
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
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

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

	var fees, bought, sold, unsold decimal.Decimal
	for _, j := range jobs {
		if _, ok := j.(*limiter.Limiter); ok {
			continue
		}
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

	start, err := time.Parse(time.RFC3339, "2023-11-01T00:00:00Z")
	if err != nil {
		return err
	}
	ndays := int(time.Now().Sub(start) / (24 * time.Hour))

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
	return nil
}
