// Copyright (c) 2023 BVK Chaitanya

package exchange

import (
	"context"
	"flag"
	"fmt"
	"slices"
	"sort"
	"time"

	"github.com/bvk/tradebot/cli"
	"github.com/bvk/tradebot/gobs"
	"github.com/bvk/tradebot/subcmds/cmdutil"
	"github.com/shopspring/decimal"
)

type Analyze struct {
	cmdutil.DBFlags

	exchange string
	product  string

	fromDate string
	numDays  int

	dropPct float64
}

func (c *Analyze) Command() (*flag.FlagSet, cli.CmdFunc) {
	fset := flag.NewFlagSet("analyze", flag.ContinueOnError)
	c.DBFlags.SetFlags(fset)
	fset.StringVar(&c.exchange, "exchange", "coinbase", "name of the exchange")
	fset.StringVar(&c.product, "product", "", "name of the trading pair")
	fset.StringVar(&c.fromDate, "from-date", "", "date of the day in YYYY-MM-DD format")
	fset.IntVar(&c.numDays, "num-days", 1, "number of the days from the date")
	fset.Float64Var(&c.dropPct, "drop-pct", 10, "percentage of high and low outliers")
	return fset, cli.CmdFunc(c.run)
}

func (c *Analyze) run(ctx context.Context, args []string) error {
	if len(args) != 0 {
		return fmt.Errorf("this command takes no arguments")
	}
	if len(c.product) == 0 {
		return fmt.Errorf("product flag cannot be empty")
	}

	if c.dropPct >= 50 {
		return fmt.Errorf("drop percentage %f is too high", c.dropPct)
	}

	if len(c.fromDate) == 0 {
		return fmt.Errorf("date argument is required")
	}
	startTime, err := time.Parse("2006-01-02", c.fromDate)
	if err != nil {
		return fmt.Errorf("could not parse date argument: %w", err)
	}
	endTime := startTime.Add(time.Duration(c.numDays) * 24 * time.Hour)

	db, err := c.DBFlags.GetDatabase(ctx)
	if err != nil {
		return fmt.Errorf("could not create database client: %w", err)
	}

	var candles []*gobs.Candle
	for s := startTime; s.Before(endTime); s = s.Add(24 * time.Hour) {
		cs, err := LoadCandles(ctx, db, c.exchange, c.product, s.Truncate(24*time.Hour))
		if err != nil {
			return fmt.Errorf("could not load candles for day %s: %w", s.Format("2006-01-02"), err)
		}
		candles = append(candles, cs...)
	}

	minute, err := c.analyze(candles)
	if err != nil {
		return fmt.Errorf("could not analyze per-minute candles: %w", err)
	}
	m15candles, err := MergeCandles(candles, 15*time.Minute)
	if err != nil {
		return fmt.Errorf("could not merge candles into 15min candles: %w", err)
	}
	m15, err := c.analyze(m15candles)
	if err != nil {
		return fmt.Errorf("could not analyze 15min candles: %w", err)
	}
	m30candles, err := MergeCandles(candles, 30*time.Minute)
	if err != nil {
		return fmt.Errorf("could not merge candles into 30min candles: %w", err)
	}
	m30, err := c.analyze(m30candles)
	if err != nil {
		return fmt.Errorf("could not analyze 30min candles: %w", err)
	}
	hcandles, err := MergeCandles(candles, time.Hour)
	if err != nil {
		return fmt.Errorf("could not merge per-minute candles into hourly candles: %w", err)
	}
	hour, err := c.analyze(hcandles)
	if err != nil {
		return fmt.Errorf("could not analyze per-hour candles: %w", err)
	}

	fmt.Printf("Product: %s\n", minute.ProductID)
	fmt.Printf("Start Time: %s\n", minute.StartTime.Format(time.RFC3339))
	fmt.Printf("End Time: %s\n", minute.EndTime.Format(time.RFC3339))
	fmt.Printf("Num candles: %d\n", minute.NumCandles)
	fmt.Printf("Candle granularity: %s\n", minute.Granularity)

	fmt.Println()
	fmt.Printf("Min Price: %s\n", minute.MinPrice.StringFixed(3))
	fmt.Printf("Max Price: %s\n", minute.MaxPrice.StringFixed(3))

	fmt.Println()
	fmt.Printf("Minimum change: %s (per min)\n", minute.Heights.Min.StringFixed(3))
	fmt.Printf("Maximum change: %s (per min)\n", minute.Heights.Max.StringFixed(3))
	fmt.Printf("Average change: %s (per min)\n", minute.Heights.Avg.StringFixed(3))
	fmt.Println()
	fmt.Printf("Minimum change: %s (per 15min)\n", m15.Heights.Min.StringFixed(3))
	fmt.Printf("Maximum change: %s (per 15min)\n", m15.Heights.Max.StringFixed(3))
	fmt.Printf("Average change: %s (per 15min)\n", m15.Heights.Avg.StringFixed(3))
	fmt.Println()
	fmt.Printf("Minimum change: %s (per 30min)\n", m30.Heights.Min.StringFixed(3))
	fmt.Printf("Maximum change: %s (per 30min)\n", m30.Heights.Max.StringFixed(3))
	fmt.Printf("Average change: %s (per 30min)\n", m30.Heights.Avg.StringFixed(3))
	fmt.Println()
	fmt.Printf("Minimum change: %s (per hour)\n", hour.Heights.Min.StringFixed(3))
	fmt.Printf("Maximum change: %s (per hour)\n", hour.Heights.Max.StringFixed(3))
	fmt.Printf("Average change: %s (per hour)\n", hour.Heights.Avg.StringFixed(3))

	fmt.Println()
	fmt.Printf("Minimum volume: %s (per min)\n", minute.Volumes.Min.StringFixed(3))
	fmt.Printf("Maximum volume: %s (per min)\n", minute.Volumes.Max.StringFixed(3))
	fmt.Printf("Average volume: %s (per min)\n", minute.Volumes.Avg.StringFixed(3))
	fmt.Println()
	fmt.Printf("Minimum volume: %s (per 15min)\n", m15.Volumes.Min.StringFixed(3))
	fmt.Printf("Maximum volume: %s (per 15min)\n", m15.Volumes.Max.StringFixed(3))
	fmt.Printf("Average volume: %s (per 15min)\n", m15.Volumes.Avg.StringFixed(3))
	fmt.Println()
	fmt.Printf("Minimum volume: %s (per 30min)\n", m30.Volumes.Min.StringFixed(3))
	fmt.Printf("Maximum volume: %s (per 30min)\n", m30.Volumes.Max.StringFixed(3))
	fmt.Printf("Average volume: %s (per 30min)\n", m30.Volumes.Avg.StringFixed(3))
	fmt.Println()
	fmt.Printf("Minimum volume: %s (per hour)\n", hour.Volumes.Min.StringFixed(3))
	fmt.Printf("Maximum volume: %s (per hour)\n", hour.Volumes.Max.StringFixed(3))
	fmt.Printf("Average volume: %s (per hour)\n", hour.Volumes.Avg.StringFixed(3))

	return nil
}

type MinMaxAvg struct {
	Min decimal.Decimal
	Max decimal.Decimal
	Avg decimal.Decimal
}

type CandleAnalysis struct {
	ProductID   string
	StartTime   time.Time
	EndTime     time.Time
	NumCandles  int
	Granularity time.Duration

	MinPrice, MaxPrice decimal.Decimal

	Heights MinMaxAvg
	Volumes MinMaxAvg
}

func (c *Analyze) analyze(candles []*gobs.Candle) (*CandleAnalysis, error) {
	result := &CandleAnalysis{
		ProductID:   c.product,
		NumCandles:  len(candles),
		Granularity: candles[0].Duration,
	}

	candles = slices.Clone(candles)

	// Sort the candles based on their start-time
	sort.Slice(candles, func(i, j int) bool {
		return candles[i].StartTime.Time.Before(candles[j].StartTime.Time)
	})

	result.StartTime = candles[0].StartTime.Time
	result.EndTime = candles[len(candles)-1].StartTime.Time.Add(candles[len(candles)-1].Duration)

	// Sort the candles based on their height.
	sort.Slice(candles, func(i, j int) bool {
		a := candles[i].High.Sub(candles[i].Low)
		b := candles[j].High.Sub(candles[j].Low)
		return a.LessThan(b)
	})

	// Drop outlier candles.
	ndrop := int(float64(len(candles)) * c.dropPct / 100)
	if v := len(candles) - 2*ndrop; v < 50 {
		return nil, fmt.Errorf("need more than 50 candles after dropping outliers")
	}
	scandles := candles[0+ndrop : len(candles)-ndrop]

	// Find the minPrice, maxPrice differences and
	var minPrice, maxPrice decimal.Decimal
	var minHeight, maxHeight, sumHeight decimal.Decimal
	var minVolume, maxVolume, sumVolume decimal.Decimal
	for i, c := range scandles {
		v := c.High.Sub(c.Low)
		if i == 0 {
			minHeight = v
			minPrice = c.Low
			minVolume = c.Volume
		}

		sumHeight = sumHeight.Add(v)
		if v.LessThan(minHeight) {
			minHeight = v
		}
		if v.GreaterThan(maxHeight) {
			maxHeight = v
		}

		sumVolume = sumVolume.Add(c.Volume)
		if c.Volume.LessThan(minVolume) {
			minVolume = c.Volume
		}
		if c.Volume.GreaterThan(maxVolume) {
			maxVolume = c.Volume
		}

		if c.Low.LessThan(minPrice) {
			minPrice = c.Low
		}
		if c.High.GreaterThan(maxPrice) {
			maxPrice = c.High
		}

	}
	nscandles := decimal.NewFromInt(int64(len(scandles)))
	avgHeight := sumHeight.Div(nscandles)
	avgVolume := sumVolume.Div(nscandles)

	// var sumVariance decimal.Decimal
	// for _, c := range scandles {
	// 	v := c.High.Sub(c.Low)
	// 	d := v.Sub(avgHeight)
	// 	sumVariance = sumVariance.Add(d.Mul(d)) // d^2
	// }
	// variance := sumVariance.Div(nscandles)

	result.MinPrice = minPrice
	result.MaxPrice = maxPrice
	result.Heights.Min = minHeight
	result.Heights.Max = maxHeight
	result.Heights.Avg = avgHeight
	result.Volumes.Min = minVolume
	result.Volumes.Max = maxVolume
	result.Volumes.Avg = avgVolume
	return result, nil
}
