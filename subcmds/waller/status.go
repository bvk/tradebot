// Copyright (c) 2023 BVK Chaitanya

package waller

import (
	"context"
	"flag"
	"fmt"
	"time"

	"github.com/bvk/tradebot/cli"
	"github.com/bvk/tradebot/exchange"
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
		return fmt.Errorf("this command takes one (key) argument")
	}

	var wall *waller.Waller
	getter := func(ctx context.Context, r kv.Reader) error {
		w, err := waller.Load(ctx, args[0], r)
		if err != nil {
			return err
		}
		wall = w
		return nil
	}

	db, err := c.Flags.GetDatabase(ctx)
	if err != nil {
		return err
	}
	if err := kv.WithReader(ctx, db, getter); err != nil {
		return err
	}

	// Print data for the waller.

	type BuyData struct {
		orders []*exchange.Order
		fees   decimal.Decimal
		size   decimal.Decimal
		value  decimal.Decimal
		feePct decimal.Decimal

		unsoldSize  decimal.Decimal
		unsoldFees  decimal.Decimal
		unsoldValue decimal.Decimal
	}
	type SellData struct {
		orders []*exchange.Order
		fees   decimal.Decimal
		size   decimal.Decimal
		value  decimal.Decimal
		feePct decimal.Decimal
	}
	type PairData struct {
		nsells int64
		nbuys  int64

		profit decimal.Decimal

		fees   decimal.Decimal
		feePct decimal.Decimal
		value  decimal.Decimal

		unsoldFees  decimal.Decimal
		unsoldSize  decimal.Decimal
		unsoldValue decimal.Decimal
	}
	type Summary struct {
		nbuys  int64
		nsells int64

		profit decimal.Decimal

		fees   decimal.Decimal
		feePct float64
		value  decimal.Decimal

		unsoldFees  decimal.Decimal
		unsoldSize  decimal.Decimal
		unsoldValue decimal.Decimal

		numDays        int
		firstOrderTime time.Time
		lastOrderTime  time.Time

		budget decimal.Decimal
		apr    decimal.Decimal
	}

	minTime := func(a, b time.Time) time.Time {
		if a.Before(b) {
			return a
		}
		return b
	}

	pairs := wall.BuySellPairs()
	buyDataMap := make(map[int]*BuyData)
	sellDataMap := make(map[int]*SellData)
	pairDataMap := make(map[int]*PairData)

	summary := &Summary{
		firstOrderTime: time.Now(),
	}
	for i, pair := range pairs {
		buys, err := wall.GetBuyOrders(pair)
		if err != nil {
			return err
		}
		sells, err := wall.GetSellOrders(pair)
		if err != nil {
			return err
		}
		if len(buys) == 0 && len(sells) == 0 {
			continue
		}

		sdata := &SellData{
			orders: sells,
		}
		var lastSellTime time.Time
		for _, sell := range sells {
			if sell.Done {
				sdata.fees = sdata.fees.Add(sell.Fee)
				sdata.size = sdata.size.Add(sell.FilledSize)
				sdata.value = sdata.value.Add(sell.FilledSize.Mul(sell.FilledPrice))
				lastSellTime = sell.CreateTime.Time

				summary.firstOrderTime = minTime(summary.firstOrderTime, sell.CreateTime.Time)
			}
		}
		if len(sells) > 0 {
			sdata.feePct = sdata.fees.Mul(Hundred).Div(sdata.value)
		}

		bdata := &BuyData{
			orders: buys,
		}
		for _, buy := range buys {
			if buy.Done {
				bdata.fees = bdata.fees.Add(buy.Fee)
				bdata.size = bdata.size.Add(buy.FilledSize)
				bdata.value = bdata.value.Add(buy.FilledSize.Mul(buy.FilledPrice))

				if buy.CreateTime.Time.After(lastSellTime) {
					bdata.unsoldFees = bdata.unsoldFees.Add(buy.Fee)
					bdata.unsoldSize = bdata.unsoldSize.Add(buy.FilledSize)
					bdata.unsoldValue = bdata.unsoldValue.Add(buy.FilledSize.Mul(buy.FilledPrice))
				}

				summary.firstOrderTime = minTime(summary.firstOrderTime, buy.CreateTime.Time)
			}
		}
		if len(buys) > 0 {
			bdata.feePct = bdata.fees.Mul(Hundred).Div(bdata.value)
		}

		pdata := &PairData{
			nbuys:  bdata.size.Div(pairs[i].Buy.Size).IntPart(),
			nsells: sdata.size.Div(pairs[i].Sell.Size).IntPart(),
			fees:   bdata.fees.Add(sdata.fees),
			value:  bdata.value.Add(sdata.value),

			unsoldFees:  bdata.unsoldFees,
			unsoldSize:  bdata.unsoldSize,
			unsoldValue: bdata.unsoldValue,
		}
		pdata.feePct = pdata.fees.Mul(Hundred).Div(pdata.value)

		if pdata.nsells > 0 {
			pdata.profit = sdata.value.Sub(sdata.fees).Sub(bdata.fees).Sub(bdata.value).Add(bdata.unsoldFees).Add(bdata.unsoldValue)
		}

		summary.nbuys += pdata.nbuys
		summary.nsells += pdata.nsells
		summary.fees = summary.fees.Add(pdata.fees)
		summary.value = summary.value.Add(pdata.value)
		summary.feePct = summary.fees.Mul(Hundred).Div(summary.value).InexactFloat64()
		summary.profit = summary.profit.Add(pdata.profit)
		summary.unsoldFees = summary.unsoldFees.Add(pdata.unsoldFees)
		summary.unsoldSize = summary.unsoldSize.Add(pdata.unsoldSize)
		summary.unsoldValue = summary.unsoldValue.Add(pdata.unsoldValue)

		pairDataMap[i] = pdata
		if len(buys) > 0 {
			buyDataMap[i] = bdata
		}
		if len(sells) > 0 {
			sellDataMap[i] = sdata
		}
	}
	summary.budget = BudgetWithFeeAt(pairs, summary.feePct)
	duration := time.Now().Sub(summary.firstOrderTime)
	numDays := int64(duration / time.Hour / 24)
	profitPerYear := summary.profit.Div(decimal.NewFromInt(numDays)).Mul(DaysPerYear)
	summary.apr = profitPerYear.Mul(Hundred).Div(summary.budget)

	fmt.Printf("Budget: %s (with fee at %.2f%%)\n", summary.budget.StringFixed(3), summary.feePct)
	for _, rate := range aprs {
		perYear := decimal.NewFromFloat(rate).Div(Hundred)
		fmt.Printf("%.1f%% Monthly Profit Goal: %s\n", rate, summary.budget.Mul(perYear).Div(MonthsPerYear).StringFixed(3))
	}
	fmt.Println()
	fmt.Printf("Profit: %s\n", summary.profit.StringFixed(3))
	fmt.Printf("Num Days: %d days\n", numDays)
	fmt.Printf("Return rate per year (projection): %s%%\n", summary.apr.StringFixed(3))
	fmt.Printf("Return rate per month (projection): %s%%\n", summary.apr.Div(MonthsPerYear).StringFixed(3))
	fmt.Println()
	fmt.Printf("Num Buys: %d\n", summary.nbuys)
	fmt.Printf("Num Sells: %d\n", summary.nsells)
	fmt.Printf("Fees: %s (%.2f%%)\n", summary.fees.StringFixed(3), summary.feePct)
	fmt.Println()
	fmt.Printf("Unsold Size: %s\n", summary.unsoldSize.StringFixed(3))
	fmt.Printf("Unsold Fees: %s\n", summary.unsoldFees.StringFixed(3))
	fmt.Printf("Unsold Value: %s\n", summary.unsoldValue.StringFixed(3))

	if c.showPairs {
		fmt.Println()
		fmt.Println("Pairs")
		for i := range pairs {
			pdata, ok := pairDataMap[i]
			if !ok {
				continue
			}
			fmt.Printf("  %s: nbuys %d nsells %d (hold %s lockin %s) fees %s feePct %s%% profit %s\n", pairs[i], pdata.nbuys, pdata.nsells, pdata.unsoldSize.StringFixed(3), pdata.unsoldValue.StringFixed(3), pdata.fees.StringFixed(3), pdata.feePct.StringFixed(3), pdata.profit.StringFixed(3))
		}
	}

	if c.showBuys {
		fmt.Println()
		fmt.Println("Buys")
		for i := range pairs {
			bdata, ok := buyDataMap[i]
			if !ok {
				continue
			}
			fmt.Printf("  %s: norders %d size %s feePct %s%% fees %s value %s\n", pairs[i].Buy, len(bdata.orders), bdata.size.StringFixed(3), bdata.feePct.StringFixed(3), bdata.fees.StringFixed(3), bdata.value.StringFixed(3))
		}
	}

	if c.showSells {
		fmt.Println()
		fmt.Println("Sells")
		for i := range pairs {
			sdata, ok := sellDataMap[i]
			if !ok {
				continue
			}
			fmt.Printf("  %s: norders %d size %s feePct %s%% fees %s value %s\n", pairs[i].Sell, len(sdata.orders), sdata.size.StringFixed(3), sdata.feePct.StringFixed(3), sdata.fees.StringFixed(3), sdata.value.StringFixed(3))
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
