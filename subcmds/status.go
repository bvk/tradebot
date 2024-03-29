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
	"github.com/bvk/tradebot/timerange"
	"github.com/bvk/tradebot/trader"
	"github.com/bvk/tradebot/waller"
	"github.com/bvkgo/kv"
	"github.com/shopspring/decimal"
)

type Status struct {
	cmdutil.DBFlags

	budget float64

	beginTime, endTime string
}

func (c *Status) Synopsis() string {
	return "Status prints global summary of all or selected trade jobs"
}

func (c *Status) Command() (*flag.FlagSet, cli.CmdFunc) {
	fset := flag.NewFlagSet("status", flag.ContinueOnError)
	c.DBFlags.SetFlags(fset)
	fset.Float64Var(&c.budget, "budget", 0, "Includes this budget in the return rate table")
	fset.StringVar(&c.beginTime, "begin-time", "", "Begin time for status time period")
	fset.StringVar(&c.endTime, "end-time", "", "End time for status time period")
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
		if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("could not load coinbase account balances: %w", err)
		}
	}
	priceMap, err := datastore.ProductsPriceMap(ctx)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			log.Printf("could not load price information (ignored): %v", err)
		}
		priceMap = make(map[string]decimal.Decimal)
	}

	var assets []string
	holdMap := make(map[string]decimal.Decimal)
	availMap := make(map[string]decimal.Decimal)
	currencyMap := make(map[string]string)
	for _, a := range accounts {
		holdMap[a.Name] = a.Hold
		availMap[a.Name] = a.Available
		currencyMap[a.Name] = a.CurrencyID
		assets = append(assets, a.Name)
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

	now := time.Now()
	parseTime := func(s string) (time.Time, error) {
		if d, err := time.ParseDuration(s); err == nil {
			return now.Add(d), nil
		}
		if v, err := time.Parse("2006-01-02", s); err == nil {
			return v, nil
		}
		return time.Parse(time.RFC3339, s)
	}

	var period timerange.Range
	if len(c.beginTime) > 0 {
		v, err := parseTime(c.beginTime)
		if err != nil {
			return err
		}
		period.Begin = v
	}
	if len(c.endTime) > 0 {
		v, err := parseTime(c.endTime)
		if err != nil {
			return err
		}
		period.End = v
	}

	// Remove jobs that don't implement Status interface.
	type Statuser interface {
		Status(*timerange.Range) *trader.Status
	}
	jobs = slices.DeleteFunc(jobs, func(j trader.Trader) bool {
		if _, ok := j.(Statuser); !ok {
			return true
		}
		return false
	})
	sort.Slice(jobs, func(i, j int) bool {
		return jobs[i].ProductID() < jobs[j].ProductID()
	})

	var statuses []*trader.Status
	for _, j := range jobs {
		if v, ok := j.(Statuser); ok {
			if s := v.Status(&period); s != nil {
				statuses = append(statuses, s)
			}
		}
	}

	sum := trader.Summarize(statuses)
	var curUnsoldValue decimal.Decimal
	for _, s := range statuses {
		if p, ok := priceMap[s.ProductID]; ok {
			curUnsoldValue = curUnsoldValue.Add(s.UnsoldSize.Mul(p))
		}
	}

	var runningStatuses []*trader.Status
	for _, s := range statuses {
		if status, ok := uid2statusMap[s.UID]; ok && status == string(job.RUNNING) {
			runningStatuses = append(runningStatuses, s)
		}
	}
	runningSum := trader.Summarize(runningStatuses)

	var (
		d30  = decimal.NewFromInt(30)
		d100 = decimal.NewFromInt(100)
		d365 = decimal.NewFromInt(365)
	)

	if period.IsZero() {
		fmt.Printf("Num Days: %s\n", sum.NumDays().StringFixed(2))
		fmt.Printf("Num Buys: %d\n", sum.NumBuys)
		fmt.Printf("Num Sells: %d\n", sum.NumSells)

		fmt.Println()
		fmt.Printf("Fees: %s\n", sum.Fees().StringFixed(3))
		fmt.Printf("Sold: %s\n", sum.Sold().StringFixed(3))
		fmt.Printf("Bought: %s\n", sum.Bought().StringFixed(3))
		fmt.Printf("Effective Fee Pct: %s%%\n", sum.FeePct().StringFixed(3))

		fmt.Println()
		fmt.Printf("Lockin Position: %s\n", curUnsoldValue.Sub(sum.UnsoldValue).StringFixed(3))
		fmt.Printf("Lockin at Buy Price: %s\n", sum.UnsoldValue.StringFixed(3))
		fmt.Printf("Lockin at Current Price: %s\n", curUnsoldValue.StringFixed(3))

		fmt.Println()
		fmt.Printf("Profit: %s\n", sum.Profit().StringFixed(3))
		fmt.Printf("Per day (average): %s\n", sum.ProfitPerDay().StringFixed(3))
		fmt.Printf("Per month (projected): %s\n", sum.ProfitPerDay().Mul(d30).StringFixed(3))
		fmt.Printf("Per year (projected): %s\n", sum.ProfitPerDay().Mul(d365).StringFixed(3))

		fmt.Println()
		fmt.Printf("Budget: %s\n", runningSum.Budget.StringFixed(3))
		fmt.Printf("Return Rate: %s%%\n", runningSum.ReturnRate().StringFixed(3))
		fmt.Printf("Annual Return Rate: %s%%\n", runningSum.AnnualReturnRate().StringFixed(3))
	}

	emptystrings := func(n int) (vs []any) {
		for i := 0; i < n; i++ {
			vs = append(vs, "")
		}
		return vs
	}

	if p := sum.Profit(); p.IsPositive() && period.IsZero() {
		fmt.Println()
		unsold, _ := sum.UnsoldValue.Float64()
		amounts := []float64{unsold, 50000, 100000, 200000, 250000, 500000}
		if c.budget != 0 {
			amounts = append([]float64{c.budget}, amounts...)
		}
		amounts = slices.DeleteFunc(amounts, func(v float64) bool { return v == 0 })

		fmtstr := strings.Repeat("%s\t", len(amounts)+1)
		amts := []any{"Amounts"}
		covered := []any{"Covered"}
		projected := []any{"Projected"}
		for _, amount := range amounts {
			c := sum.Profit().Mul(d100).Div(decimal.NewFromFloat(amount))
			p := sum.ProfitPerDay().Mul(d365).Mul(d100).Div(decimal.NewFromFloat(amount))
			amts = append(amts, fmt.Sprintf("%.02f", amount))
			covered = append(covered, fmt.Sprintf("%s%%", c.StringFixed(3)))
			projected = append(projected, fmt.Sprintf("%s%%", p.StringFixed(3)))
		}
		tw := tabwriter.NewWriter(os.Stdout, 0, 0, 1, ' ', tabwriter.AlignRight)
		fmt.Fprintf(tw, fmtstr+"\n", amts...)
		fmt.Fprintf(tw, fmtstr+"\n", covered...)
		fmt.Fprintf(tw, fmtstr+"\n", projected...)
		tw.Flush()
	}

	if len(availMap) > 0 && period.IsZero() {
		fmt.Println()
		ids := []any{""}
		avails := []any{"Available"}
		holds := []any{"Hold"}
		prices := []any{"Price"}
		totals := []any{"Total"}
		for _, a := range assets {
			currency := currencyMap[a]
			ids = append(ids, a)
			if p, ok := priceMap[currency+"-USD"]; ok {
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
			astatus, bstatus := uid2statusMap[a.UID], uid2statusMap[b.UID]
			if astatus == bstatus {
				return uid2nameMap[a.UID] < uid2nameMap[b.UID]
			}
			return slices.Index(order, astatus) < slices.Index(order, bstatus)
		})

		fmt.Println()
		tw := tabwriter.NewWriter(os.Stdout, 0, 0, 1, ' ', tabwriter.AlignRight)
		fmt.Fprintf(tw, "Name/UID\tStatus\tProduct\tBudget\tReturn\tAnnualReturn\tDays\tBuys\tSells\tProfit\tFees\tBoughtValue\tSoldValue\tUnsoldValue\tSoldSize\tUnsoldSize\t\n")
		for _, s := range statuses {
			name := uid2nameMap[s.UID]
			status := uid2statusMap[s.UID]
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s%%\t%s%%\t%s\t%d\t%d\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t\n", name, status, s.ProductID, s.Budget.StringFixed(3), s.ReturnRate().StringFixed(3), s.AnnualReturnRate().StringFixed(3), s.NumDays().StringFixed(2), s.NumBuys, s.NumSells, s.Profit().StringFixed(3), s.Fees().StringFixed(3), s.Bought().StringFixed(3), s.Sold().StringFixed(3), s.UnsoldValue.StringFixed(3), s.SoldSize.Sub(s.OversoldSize).StringFixed(3), s.UnsoldSize.StringFixed(3))
		}
		tw.Flush()
	}
	return nil
}
