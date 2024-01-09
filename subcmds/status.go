// Copyright (c) 2023 BVK Chaitanya

package subcmds

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"slices"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/bvk/tradebot/cli"
	"github.com/bvk/tradebot/coinbase"
	"github.com/bvk/tradebot/job"
	"github.com/bvk/tradebot/limiter"
	"github.com/bvk/tradebot/looper"
	"github.com/bvk/tradebot/namer"
	"github.com/bvk/tradebot/server"
	"github.com/bvk/tradebot/subcmds/cmdutil"
	"github.com/bvk/tradebot/trader"
	"github.com/bvk/tradebot/waller"
	"github.com/bvkgo/kv"
	"github.com/shopspring/decimal"
)

type Status struct {
	cmdutil.DBFlags
}

func (c *Status) Synopsis() string {
	return "Status prints global summary of all or selected trade jobs"
}

func (c *Status) Command() (*flag.FlagSet, cli.CmdFunc) {
	fset := flag.NewFlagSet("status", flag.ContinueOnError)
	c.DBFlags.SetFlags(fset)
	return fset, cli.CmdFunc(c.run)
}

func (c *Status) run(ctx context.Context, args []string) error {
	db, closer, err := c.DBFlags.GetDatabase(ctx)
	if err != nil {
		return err
	}
	defer closer()

	datastore := coinbase.NewDatastore(db)
	accounts, err := datastore.LoadAccounts(ctx)
	if err != nil {
		return fmt.Errorf("could not load coinbase account balances: %w", err)
	}
	priceMap, err := datastore.PriceMapAt(ctx, time.Now())
	if err != nil || len(priceMap) == 0 {
		log.Printf("could not load price information (ignored): %v", err)
	}

	var assets []string
	holdMap := make(map[string]decimal.Decimal)
	availMap := make(map[string]decimal.Decimal)
	for _, a := range accounts {
		holdMap[a.CurrencyID] = a.Hold
		availMap[a.CurrencyID] = a.Available
		assets = append(assets, a.CurrencyID)
	}
	sort.Strings(assets)

	var jobs []trader.Trader
	uid2nameMap := make(map[string]string)
	uid2statusMap := make(map[string]string)
	load := func(ctx context.Context, r kv.Reader) error {
		if len(args) > 0 {
			for _, arg := range args {
				_, uid, typename, err := namer.Resolve(ctx, r, arg)
				if err != nil {
					if !errors.Is(err, os.ErrNotExist) {
						return fmt.Errorf("could not resolve job type %q: %w", arg, err)
					}
					uid = arg
				}
				job, err := server.Load(ctx, r, uid, typename)
				if err != nil {
					return fmt.Errorf("could not load job with uid %q: %w", uid, err)
				}
				jobs = append(jobs, job)
			}
		} else {
			vs, err := server.LoadTraders(ctx, r)
			if err != nil {
				return fmt.Errorf("could not load traders: %w", err)
			}
			jobs = vs
		}

		for _, j := range jobs {
			uid := j.UID()
			uid = strings.TrimPrefix(uid, limiter.DefaultKeyspace)
			uid = strings.TrimPrefix(uid, looper.DefaultKeyspace)
			uid = strings.TrimPrefix(uid, waller.DefaultKeyspace)
			name := uid
			if v, _, _, err := namer.Resolve(ctx, r, uid); err == nil {
				name = v
			}
			uid2nameMap[j.UID()] = name
			status := "UNKNOWN"
			if v, err := job.StatusDB(ctx, db, j.UID()); err == nil {
				status = string(v)
			}
			uid2statusMap[uid] = status
		}
		return nil
	}
	if err := kv.WithReader(ctx, db, load); err != nil {
		return err
	}

	// Remove limiter jobs cause they are one-side trades.
	jobs = slices.DeleteFunc(jobs, func(j trader.Trader) bool {
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

	sum := trader.Summarize(statuses)
	// js, _ := json.MarshalIndent(sum, "", "  ")
	// fmt.Printf("%s\n", js)

	var (
		d30  = decimal.NewFromInt(30)
		d100 = decimal.NewFromInt(100)
		d365 = decimal.NewFromInt(365)
	)

	fmt.Printf("Num Days: %d\n", sum.NumDays())
	fmt.Printf("Num Buys: %d\n", sum.NumBuys)
	fmt.Printf("Num Sells: %d\n", sum.NumSells)

	fmt.Println()
	fmt.Printf("Fees: %s\n", sum.Fees().StringFixed(3))
	fmt.Printf("Sold: %s\n", sum.Sold().StringFixed(3))
	fmt.Printf("Bought: %s\n", sum.Bought().StringFixed(3))
	fmt.Printf("Lockin: %s\n", sum.UnsoldValue.StringFixed(3))
	fmt.Printf("Effective Fee Pct: %s%%\n", sum.FeePct().StringFixed(3))

	fmt.Println()
	fmt.Printf("Profit: %s\n", sum.Profit().StringFixed(3))
	fmt.Printf("Per day (average): %s\n", sum.ProfitPerDay().StringFixed(3))
	fmt.Printf("Per month (projected): %s\n", sum.ProfitPerDay().Mul(d30).StringFixed(3))
	fmt.Printf("Per year (projected): %s\n", sum.ProfitPerDay().Mul(d365).StringFixed(3))

	fmt.Println()
	fmt.Printf("Budget: %s\n", sum.Budget.StringFixed(3))
	fmt.Printf("Return Rate: %s%%\n", sum.ReturnRate().StringFixed(3))
	fmt.Printf("Annual Return Rate: %s%%\n", sum.AnnualReturnRate().StringFixed(3))

	emptystrings := func(n int) (vs []any) {
		for i := 0; i < n; i++ {
			vs = append(vs, "")
		}
		return vs
	}

	if p := sum.Profit(); p.IsPositive() {
		fmt.Println()
		rates := []float64{2.625, 5, 8, 10, 15, 20}
		fmtstr := strings.Repeat("%s\t", len(rates)+1)
		aprs := []any{"ARR"}
		covered := []any{"Covered"}
		projected := []any{"Projected"}
		for _, rate := range rates {
			c := sum.Profit().Div(decimal.NewFromFloat(rate).Div(d100))
			p := sum.ProfitPerDay().Mul(d365).Div(decimal.NewFromFloat(rate).Div(d100))
			aprs = append(aprs, fmt.Sprintf("%.03f%%", rate))
			covered = append(covered, c.StringFixed(3))
			projected = append(projected, p.StringFixed(3))
		}
		tw := tabwriter.NewWriter(os.Stdout, 0, 0, 1, ' ', tabwriter.AlignRight)
		fmt.Fprintf(tw, fmtstr+"\n", aprs...)
		fmt.Fprintf(tw, fmtstr+"\n", covered...)
		fmt.Fprintf(tw, fmtstr+"\n", projected...)
		tw.Flush()
	}

	if len(availMap) > 0 {
		fmt.Println()
		ids := []any{""}
		avails := []any{"Available"}
		holds := []any{"Hold"}
		prices := []any{"Price"}
		totals := []any{"Total"}
		for _, a := range assets {
			ids = append(ids, a)
			if p, ok := priceMap[a+"-USD"]; ok {
				prices = append(prices, p.StringFixed(3))
			} else {
				prices = append(prices, "")
			}
			hold, avail := holdMap[a], availMap[a]
			holds = append(holds, hold.StringFixed(3))
			avails = append(avails, avail.StringFixed(3))
			totals = append(totals, hold.Add(avail).StringFixed(3))
		}
		tw := tabwriter.NewWriter(os.Stdout, 0, 0, 1, ' ', tabwriter.AlignRight)
		fmtstr := strings.Repeat("%s\t", len(assets)+1)
		fmt.Fprintf(tw, fmtstr+"\n", ids...)
		fmt.Fprintf(tw, fmtstr+"\n", holds...)
		fmt.Fprintf(tw, fmtstr+"\n", avails...)
		fmt.Fprintf(tw, fmtstr+"\n", totals...)
		fmt.Fprintf(tw, fmtstr+"\n", emptystrings(len(assets)+1)...)
		fmt.Fprintf(tw, fmtstr+"\n", prices...)
		tw.Flush()
	}

	if len(statuses) > 0 {
		order := []string{"RUNNING", "PAUSED", "COMPLETED", "FAILED", "CANCELED"}
		sort.Slice(statuses, func(i, j int) bool {
			a, b := statuses[i], statuses[j]
			astatus, bstatus := uid2statusMap[a.UID()], uid2statusMap[b.UID()]
			if astatus == bstatus {
				return a.ProductID < b.ProductID
			}
			return slices.Index(order, astatus) < slices.Index(order, bstatus)
		})

		fmt.Println()
		tw := tabwriter.NewWriter(os.Stdout, 0, 0, 1, ' ', tabwriter.AlignRight)
		fmt.Fprintf(tw, "Name/UID\tStatus\tProduct\tBudget\tReturn\tAnnualReturn\tDays\tBuys\tSells\tProfit\tFees\tBoughtValue\tSoldValue\tUnsoldValue\tSoldSize\tUnsoldSize\t\n")
		for _, s := range statuses {
			uid := s.UID()
			name := uid2nameMap[uid]
			status := uid2statusMap[uid]
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s%%\t%s%%\t%d\t%d\t%d\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t\n", name, status, s.ProductID, s.Budget.StringFixed(3), s.ReturnRate().StringFixed(3), s.AnnualReturnRate().StringFixed(3), s.NumDays(), s.NumBuys, s.NumSells, s.Profit().StringFixed(3), s.Fees().StringFixed(3), s.Bought().StringFixed(3), s.Sold().StringFixed(3), s.UnsoldValue.StringFixed(3), s.SoldSize.Sub(s.OversoldSize).StringFixed(3), s.UnsoldSize.StringFixed(3))
		}
		tw.Flush()
	}
	return nil
}
