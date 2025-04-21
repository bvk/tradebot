// Copyright (c) 2025 BVK Chaitanya

package waller

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"

	"github.com/bvk/tradebot/point"
	"github.com/bvk/tradebot/waller"
	"github.com/shopspring/decimal"
	"github.com/visvasity/cli"
)

type Simulate struct {
	spec Spec
}

func (c *Simulate) check() error {
	if err := c.spec.Check(); err != nil {
		return err
	}
	return nil
}

func (c *Simulate) Run(ctx context.Context, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("this command takes one prices file argument")
	}
	if err := c.check(); err != nil {
		return err
	}

	// Read price data from the input file.
	data, err := os.ReadFile(args[0])
	if err != nil {
		return err
	}
	values := strings.Fields(string(data))
	prices := make([]decimal.Decimal, len(values))
	for i, value := range values {
		p, err := strconv.ParseFloat(value, 10)
		if err != nil {
			return fmt.Errorf("could not parse %q as a float: %w", value, err)
		}
		prices[i] = decimal.NewFromFloat(p)
	}

	pairs := c.spec.BuySellPairs()
	analysis := waller.Analyze(pairs, c.spec.feePercentage)

	// Initialize the simulator with all points as outstanding buys.
	buys := make(map[*point.Pair]struct{})
	for _, p := range pairs {
		buys[p] = struct{}{}
	}

	var profit, lastPrice decimal.Decimal
	sells := make(map[*point.Pair]struct{})
	for i, price := range prices {
		if i == 0 {
			lastPrice = price
			continue
		}

		// Buys and sells can both be executed when the price is increasing, but only
		// buys can happen when the price is decreasing.

		if lastPrice.LessThan(price) {
			for _, p := range pairs {
				if _, ok := buys[p]; ok && lastPrice.LessThan(p.Buy.Price) && p.Buy.Price.LessThan(price) {
					delete(buys, p)
					sells[p] = struct{}{}
					// log.Printf("buy executed for point %v at price %v->%v", p, lastPrice, price)
				}
				if _, ok := sells[p]; ok && lastPrice.LessThan(p.Sell.Price) && p.Sell.Price.LessThan(price) {
					delete(sells, p)
					buys[p] = struct{}{}
					profit = profit.Add(p.Sell.Size.Mul(p.Sell.Price.Sub(p.Buy.Price)))
					profit = profit.Sub(p.Buy.FeeAt(c.spec.feePercentage)).Sub(p.Sell.FeeAt(c.spec.feePercentage))
					log.Printf("sell executed for point %v at price %v->%v (profit=%v)", p, lastPrice, price, profit)
				}
			}
		}

		if price.LessThan(lastPrice) {
			for _, p := range pairs {
				if _, ok := buys[p]; ok && price.LessThan(p.Buy.Price) && p.Buy.Price.LessThan(lastPrice) {
					delete(buys, p)
					sells[p] = struct{}{}
					// log.Printf("buy executed for point %v at price %v->%v", p, lastPrice, price)
				}
			}
		}

		lastPrice = price
	}

	fmt.Printf("Budget: %s\n", analysis.Budget().StringFixed(3))
	fmt.Printf("Profit: %s\n", profit.StringFixed(3))
	return nil
}

func (c *Simulate) Command() (string, *flag.FlagSet, cli.CmdFunc) {
	fset := flag.NewFlagSet("simulate", flag.ContinueOnError)
	c.spec.SetFlags(fset)
	return "simulate", fset, cli.CmdFunc(c.Run)
}

func (c *Simulate) Purpose() string {
	return "Runs a waller simulation over the input price data from a file"
}

func (c *Simulate) Description() string {
	return `

Command "simulate" runs a simulation to determine how much profit would be
generated if a waller job has run over the input ticker price data.

Input ticker price data is taken in the form of a file argument. File should
contain prices as floating point values separated by spaces (or newlines).

`
}
