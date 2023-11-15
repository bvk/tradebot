// Copyright (c) 2023 BVK Chaitanya

package exchange

import (
	"context"
	"flag"
	"fmt"
	"sort"
	"time"

	"github.com/bvk/tradebot/api"
	"github.com/bvk/tradebot/cli"
	"github.com/bvk/tradebot/gobs"
	"github.com/bvk/tradebot/subcmds"
	"github.com/shopspring/decimal"
)

type Analyze struct {
	subcmds.ClientFlags

	name    string
	product string

	start string
	end   string

	feePct  float64
	dropPct float64
}

func (c *Analyze) Command() (*flag.FlagSet, cli.CmdFunc) {
	fset := flag.NewFlagSet("analyze", flag.ContinueOnError)
	c.ClientFlags.SetFlags(fset)
	fset.StringVar(&c.name, "name", "coinbase", "name of the exchange")
	fset.StringVar(&c.product, "product", "BCH-USD", "name of the trading pair")
	fset.StringVar(&c.start, "start", "", "start time for the candles")
	fset.StringVar(&c.end, "end", "", "end time for the candles")
	fset.Float64Var(&c.dropPct, "drop-pct", 10, "percentage of high and low outliers")
	fset.Float64Var(&c.feePct, "fee-pct", 0.25, "exchange fee percentage")
	return fset, cli.CmdFunc(c.run)
}

func (c *Analyze) getCandles(ctx context.Context, start, end time.Time) ([]*gobs.Candle, error) {
	var candles []*gobs.Candle
	req := &api.ExchangeGetCandlesRequest{
		ExchangeName: c.name,
		ProductID:    c.product,
		StartTime:    start,
		EndTime:      end,
	}
	for {
		resp, err := subcmds.Post[api.ExchangeGetCandlesResponse](ctx, &c.ClientFlags, "/exchange/get-candles", req)
		if err != nil {
			return nil, fmt.Errorf("POST request to exchange/get-candles failed: %w", err)
		}
		candles = append(candles, resp.Candles...)
		if resp.Continue == nil {
			break
		}
		req = resp.Continue
	}
	return candles, nil
}

func (c *Analyze) run(ctx context.Context, args []string) error {
	if len(args) != 0 {
		return fmt.Errorf("this command takes no arguments")
	}

	if c.dropPct >= 50 {
		return fmt.Errorf("drop percentage %q is too high", c.dropPct)
	}

	if c.start == "" {
		return fmt.Errorf("start time flag is required")
	}
	startTime, err := time.Parse(time.RFC3339, c.start)
	if err != nil {
		return fmt.Errorf("could not parse start time as RFC3339 value: %w", err)
	}

	endTime := time.Now()
	if c.end != "" {
		v, err := time.Parse(time.RFC3339, c.end)
		if err != nil {
			return fmt.Errorf("could not parse end time as RFC3339 value: %w", err)
		}
		endTime = v
	}

	candles, err := c.getCandles(ctx, startTime, endTime)
	if err != nil {
		return fmt.Errorf("could not fetch candles in the time range: %w", err)
	}

	// Analyze the candles:
	//
	// 1. Sort all candles height-wise.
	//
	// 2. Drop 10% outliers on the front and back.
	//
	// 3. Find the minimum profitable margin for the candles
	//

	startTime = candles[0].StartTime.Time
	endTime = candles[len(candles)-1].EndTime.Time
	sort.Slice(candles, func(i, j int) bool {
		a := candles[i].High.Sub(candles[i].Low)
		b := candles[j].High.Sub(candles[j].Low)
		return a.LessThan(b)
	})

	ndrop := int(float64(len(candles)) * c.dropPct / 100)
	if v := len(candles) - 2*ndrop; v < 50 {
		return fmt.Errorf("need more than 50 candles after dropping outliers")
	}

	scandles := candles[0+ndrop : len(candles)-ndrop]
	var minPrice, maxPrice, minVolty, maxVolty, sumVolty decimal.Decimal
	for i, c := range scandles {
		v := c.High.Sub(c.Low)
		if i == 0 {
			minVolty = v
			minPrice = c.Low
		}
		sumVolty = sumVolty.Add(v)
		if v.LessThan(minVolty) {
			minVolty = v
		}
		if v.GreaterThan(maxVolty) {
			maxVolty = v
		}
		if c.Low.LessThan(minPrice) {
			minPrice = c.Low
		}
		if c.High.GreaterThan(maxPrice) {
			maxPrice = c.High
		}
	}
	nscandles := decimal.NewFromInt(int64(len(scandles)))
	avgVolty := sumVolty.Div(nscandles)

	// var sumVariance decimal.Decimal
	// for _, c := range scandles {
	// 	v := c.High.Sub(c.Low)
	// 	d := v.Sub(avgVolty)
	// 	sumVariance = sumVariance.Add(d.Mul(d)) // d^2
	// }
	// variance := sumVariance.Div(nscandles)

	hundred := decimal.NewFromInt(100)
	feePct := decimal.NewFromFloat(c.feePct)
	minFee := minPrice.Mul(feePct).Div(hundred)
	maxFee := maxPrice.Mul(feePct).Div(hundred)

	fmt.Printf("Product: %s\n", c.product)
	fmt.Printf("Start Time: %s\n", startTime)
	fmt.Printf("End Time: %s\n", endTime)
	fmt.Println()
	// fmt.Printf("Volatity Variance: %s\n", variance.StringFixed(3))
	fmt.Printf("Minimum Volatity: %s\n", minVolty.StringFixed(3))
	fmt.Printf("Maximum Volatity: %s\n", maxVolty.StringFixed(3))
	fmt.Printf("Average Volatity: %s\n", avgVolty.StringFixed(3))
	fmt.Println()
	fmt.Printf("Minimum fee: %s\n", minFee.StringFixed(3))
	fmt.Printf("Maximum fee: %s\n", maxFee.StringFixed(3))

	return nil
}
