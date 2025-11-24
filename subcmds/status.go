// Copyright (c) 2023 BVK Chaitanya

package subcmds

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"slices"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/bvk/tradebot/coinbase"
	"github.com/bvk/tradebot/coinex"
	"github.com/bvk/tradebot/gobs"
	"github.com/bvk/tradebot/job"
	"github.com/bvk/tradebot/looper"
	"github.com/bvk/tradebot/namer"
	"github.com/bvk/tradebot/server"
	"github.com/bvk/tradebot/subcmds/cmdutil"
	"github.com/bvk/tradebot/timerange"
	"github.com/bvk/tradebot/waller"
	"github.com/bvkgo/kv"
	"github.com/shopspring/decimal"
	"github.com/visvasity/cli"
)

type Status struct {
	cmdutil.DBFlags

	beginTime, endTime string

	pricesFrom string

	accounts bool

	fixSummary bool
}

func (c *Status) Purpose() string {
	return "Prints summary of all or selected trade jobs"
}

func (c *Status) Command() (string, *flag.FlagSet, cli.CmdFunc) {
	fset := flag.NewFlagSet("status", flag.ContinueOnError)
	c.DBFlags.SetFlags(fset)
	fset.StringVar(&c.beginTime, "begin-time", "", "Begin time for status time period")
	fset.StringVar(&c.endTime, "end-time", "", "End time for status time period")
	fset.StringVar(&c.pricesFrom, "prices-from", "", "Deprecated flag; does nothing.")
	fset.BoolVar(&c.accounts, "accounts", false, "When true, print account balances from the datastore.")
	fset.BoolVar(&c.fixSummary, "fix-summary", false, "When true, job summary is updated for non-running jobs in the db.")
	return "status", fset, cli.CmdFunc(c.run)
}

func (c *Status) run(ctx context.Context, args []string) error {
	// Prepare a time-period if it was given.
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

	// Open the database.
	db, closer, err := c.DBFlags.GetDatabase(ctx)
	if err != nil {
		return err
	}
	defer closer()

	runner := job.NewRunner(db)
	exchanges := []string{}
	uid2nameMap := make(map[string]string)
	uid2jdMap := make(map[string]*gobs.JobData)
	uid2sumMap := make(map[string]*gobs.Summary)
	collectJobs := func(ctx context.Context, r kv.Reader, jd *gobs.JobData) error {
		name, _, _, err := namer.Resolve(ctx, r, jd.ID)
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				return err
			}
			name = jd.ID
		}
		if len(args) != 0 && !slices.Contains(args, name) && !slices.Contains(args, jd.ID) {
			return nil // Skip the job
		}

		p := &period
		if period.IsZero() {
			p = nil
		}

		var sum *gobs.Summary
		switch {
		case strings.EqualFold(jd.Typename, "waller"):
			sum, err = waller.Summary(ctx, r, jd.ID, p)
			if err != nil {
				return err
			}
		case strings.EqualFold(jd.Typename, "looper"):
			sum, err = looper.Summary(ctx, r, jd.ID, p)
			if err != nil {
				return err
			}
		default:
			return nil // Skip the job
		}

		// Adjust to reflect the user chosen time periods.
		if !period.IsZero() {
			sum.BeginAt, sum.EndAt = period.Begin, period.End
		} else {
			sum.EndAt = time.Now()
			if sum.BeginAt.IsZero() {
				sum.BeginAt = sum.EndAt
			}
		}

		uid2jdMap[jd.ID] = jd
		uid2sumMap[jd.ID] = sum
		uid2nameMap[jd.ID] = name
		if !slices.Contains(exchanges, sum.Exchange) {
			exchanges = append(exchanges, sum.Exchange)
		}
		return nil
	}
	if err := runner.Scan(ctx, nil, collectJobs); err != nil {
		return err
	}

	if c.fixSummary {
		for _, jd := range uid2jdMap {
			jd := jd
			if jd.State.IsRunning() {
				slog.Warn("job is currently running, so summary is not updated (skipped)", "uid", jd.ID)
				continue
			}
			fix := func(ctx context.Context, rw kv.ReadWriter) error {
				job, err := server.Load(ctx, rw, jd.ID, jd.Typename)
				if err != nil {
					return fmt.Errorf("could not load traders: %w", err)
				}
				if err := job.Save(ctx, rw); err != nil {
					slog.Error("could not save job", "job", job, "err", err)
					return err
				}
				return nil
			}
			if err := kv.WithReadWriter(ctx, db, fix); err != nil {
				return fmt.Errorf("could not apply summary fix: %w", err)
			}
		}
	}

	// Collect current prices.
	exchangePriceMap := make(map[string]map[string]decimal.Decimal)
	exchangeProductPriceMap := make(map[string]map[string]decimal.Decimal)
	if slices.Contains(exchanges, "coinbase") {
		pmap, err := coinbase.GetProductPriceMap(ctx)
		if err != nil {
			slog.Warn("could not get product pricing from coinbase (ignored)", "err", err)
		}
		exchangeProductPriceMap["coinbase"] = pmap
		exchangePriceMap["coinbase"] = coinbase.GetPriceMap(pmap)
	}
	if slices.Contains(exchanges, "coinex") {
		pmap, err := coinex.GetProductPriceMap(ctx)
		if err != nil {
			slog.Warn("could not get product pricing from coinex (ignored)", "err", err)
		}
		exchangeProductPriceMap["coinex"] = pmap
		exchangePriceMap["coinex"] = coinex.GetPriceMap(pmap)
	}

	var d30 = decimal.NewFromInt(30)
	var d100 = decimal.NewFromInt(100)
	var d365 = decimal.NewFromInt(365)

	// Print overall summary when no time period is specified and all jobs are requested.
	if period.IsZero() && len(args) == 0 {
		allSum := new(gobs.Summary)
		runningSum := new(gobs.Summary)
		var curUnsoldValue decimal.Decimal
		for uid, sum := range uid2sumMap {
			allSum.Add(sum)
			if uid2jdMap[uid].State.IsRunning() {
				runningSum.Add(sum)
			}
			if p, ok := exchangeProductPriceMap[sum.Exchange][sum.ProductID]; ok {
				curUnsoldValue = curUnsoldValue.Add(sum.UnsoldSize.Mul(p))
			}
		}
		allSum.EndAt = time.Now()
		runningSum.EndAt = time.Now()

		fmt.Printf("Num Days: %s\n", allSum.NumDays().StringFixed(2))
		fmt.Printf("Num Buys: %s\n", allSum.NumBuys.StringFixed(1))
		fmt.Printf("Num Sells: %s\n", allSum.NumSells.StringFixed(1))

		fmt.Println()
		fmt.Printf("Bought Value: %s\n", allSum.BoughtValue.StringFixed(3))
		fmt.Printf("Sold Value: %s\n", allSum.SoldValue.StringFixed(3))
		fmt.Printf("Unsold Value: %s\n", allSum.UnsoldValue.StringFixed(3))
		fmt.Printf("Oversold Value: %s\n", allSum.OversoldValue.StringFixed(3))

		fmt.Println()
		fmt.Printf("Bought Fees: %s\n", allSum.BoughtFees.StringFixed(3))
		fmt.Printf("Sold Fees: %s\n", allSum.SoldFees.StringFixed(3))
		fmt.Printf("Unsold Fees: %s\n", allSum.UnsoldFees.StringFixed(3))
		fmt.Printf("Oversold Fees: %s\n", allSum.OversoldFees.StringFixed(3))
		fmt.Printf("Total Fees: %s\n", allSum.Fees().StringFixed(3))
		fmt.Printf("Effective Fee Pct: %s%%\n", allSum.FeePct().StringFixed(3))

		fmt.Println()
		fmt.Printf("Lifetime Profit: %s\n", allSum.Profit().StringFixed(3))
		fmt.Printf("Lifetime Profit Per Day: %s\n", allSum.ProfitPerDay().StringFixed(3))
		fmt.Printf("Lifetime Profit Per Month: %s\n", allSum.ProfitPerDay().Mul(d30).StringFixed(3))
		fmt.Printf("Lifetime Profit Per Year: %s\n", allSum.ProfitPerDay().Mul(d365).StringFixed(3))

		fmt.Println()
		curUnsoldPosition := curUnsoldValue.Sub(allSum.UnsoldValue)
		fmt.Printf("Unsold Position: %s\n", curUnsoldPosition.StringFixed(3))
		fmt.Printf("Unsold Bought Value: %s\n", allSum.UnsoldValue.StringFixed(3))
		fmt.Printf("Unsold Current Value: %s\n", curUnsoldValue.StringFixed(3))

		fmt.Println()
		profitPosition := allSum.Profit().Add(curUnsoldPosition)
		profitPositionPerDay := profitPosition.Div(allSum.NumDays())
		fmt.Printf("Profit w Unsold Position: %s\n", profitPosition.StringFixed(3))
		if !allSum.UnsoldValue.IsZero() {
			fmt.Printf("Return Percent w Unsold Value: %s%%\n", profitPosition.Div(allSum.UnsoldValue).Mul(d100).StringFixed(3))
			fmt.Printf("Annual Return Percent w Unsold Value: %s%%\n", profitPositionPerDay.Mul(d365).Div(allSum.UnsoldValue).Mul(d100).StringFixed(3))
		}

		fmt.Println()
		fmt.Printf("Running Profit: %s\n", runningSum.Profit().StringFixed(3))
		fmt.Printf("Running Profit Per Day: %s\n", runningSum.ProfitPerDay().StringFixed(3))
		fmt.Printf("Running Profit Per Month: %s\n", runningSum.ProfitPerDay().Mul(d30).StringFixed(3))
		fmt.Printf("Running Profit Per Year: %s\n", runningSum.ProfitPerDay().Mul(d365).StringFixed(3))

		fmt.Println()
		fmt.Printf("Running Profit: %s\n", runningSum.Profit().StringFixed(3))
		fmt.Printf("Running Budget: %s\n", runningSum.Budget.StringFixed(3))
		fmt.Printf("Running Return Percent: %s%%\n", runningSum.ReturnPct().StringFixed(3))
		fmt.Printf("Running Annual Return Percent: %s%%\n", runningSum.AnnualPct().StringFixed(3))
	}

	// FIXME: The following doesn't work for CoinEx.
	if c.accounts {
		// Fetch last known account balances.
		datastore := coinbase.NewDatastore(db)
		accounts, err := datastore.LoadAccounts(ctx)
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("could not load coinbase account balances: %w", err)
			}
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
		emptystrings := func(n int) (vs []any) {
			for i := 0; i < n; i++ {
				vs = append(vs, "")
			}
			return vs
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
				if priceMap, ok := exchangePriceMap["coinbase"]; ok {
					if p, ok := priceMap[currency]; ok {
						prices = append(prices, p.StringFixed(3))
					} else {
						prices = append(prices, "")
					}
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
			fmt.Fprintln(tw)
			tw.Flush()
		}
	}

	// Pick a job order.
	uids := slices.Collect(maps.Keys(uid2jdMap))
	order := []gobs.State{gobs.RUNNING, gobs.PAUSED, gobs.COMPLETED, gobs.FAILED, gobs.CANCELED}
	sort.SliceStable(uids, func(i, j int) bool {
		is, js := uid2jdMap[uids[i]].State, uid2jdMap[uids[j]].State
		if x, y := slices.Index(order, is), slices.Index(order, js); x != y {
			return x < y
		}
		in, _ := uid2nameMap[uids[i]]
		jn, _ := uid2nameMap[uids[j]]
		if in != jn {
			return in < jn
		}
		return uid2sumMap[uids[i]].ProductID < uid2sumMap[uids[j]].ProductID
	})

	// Print the job summary table.
	fmt.Println()
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 1, ' ', tabwriter.AlignRight)
	fmt.Fprintf(tw, "Name/UID\tStatus\tProduct\tBudget\tReturn\tAnnualReturn\tDays\tBuys\tSells\tProfit\tBoughtValue\tBoughtSize\tBoughtFees\tSoldValue\tSoldSize\tSoldFees\tUnsoldValue\tUnsoldSize\tUnsoldFees\tOversoldValue\tOversoldSize\tOversoldFees\t\n")
	for _, uid := range uids {
		jd := uid2jdMap[uid]
		name, ok := uid2nameMap[uid]
		if !ok {
			name = jd.ID
		}
		s := uid2sumMap[uid]
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s%%\t%s%%\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t\n", name, jd.State, s.ProductID, s.Budget.StringFixed(3), s.ReturnPct().StringFixed(3), s.AnnualPct().StringFixed(3), s.NumDays().StringFixed(2), s.NumBuys.StringFixed(1), s.NumSells.StringFixed(1), s.Profit().StringFixed(3), s.BoughtValue.StringFixed(3), s.BoughtSize.StringFixed(3), s.BoughtFees.StringFixed(3), s.SoldValue.StringFixed(3), s.SoldSize.StringFixed(3), s.SoldFees.StringFixed(3), s.UnsoldValue.StringFixed(3), s.UnsoldSize.StringFixed(3), s.UnsoldFees.StringFixed(3), s.OversoldValue.StringFixed(3), s.OversoldSize.StringFixed(3), s.OversoldFees.StringFixed(3))
	}
	tw.Flush()
	return nil
}
