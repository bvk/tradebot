// Copyright (c) 2023 BVK Chaitanya

package waller

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"

	"github.com/bvkgo/tradebot/api"
	"github.com/bvkgo/tradebot/cli"
	"github.com/bvkgo/tradebot/point"
	"github.com/bvkgo/tradebot/subcmds"
	"github.com/shopspring/decimal"
)

type Add struct {
	fset *flag.FlagSet

	subcmds.ClientFlags

	dryRun bool

	product string

	beginPriceRange float64
	endPriceRange   float64

	buyInterval float64
	sellMargin  float64

	buySize  float64
	sellSize float64

	buyCancelOffset  float64
	sellCancelOffset float64
}

func (c *Add) check() error {
	if c.beginPriceRange <= 0 || c.endPriceRange <= 0 {
		return fmt.Errorf("begin/end price ranges cannot be zero or negative")
	}
	if c.buySize <= 0 || c.sellSize <= 0 {
		return fmt.Errorf("buy/sell sizes cannot be zero or negative")
	}
	if c.buyInterval <= 0 {
		return fmt.Errorf("buy interval cannot be zero or negative")
	}
	if c.buyCancelOffset <= 0 || c.sellCancelOffset <= 0 {
		return fmt.Errorf("buy/sell cancel offsets cannot be zero or negative")
	}
	if c.sellMargin <= 0 {
		return fmt.Errorf("sell margin cannot be zero or negative")
	}

	if c.endPriceRange <= c.beginPriceRange {
		return fmt.Errorf("end price range cannot be lower or equal to the begin price")
	}
	if diff := c.endPriceRange - c.beginPriceRange; diff <= c.buyInterval {
		return fmt.Errorf("price range %f is too small for the buy interval %f", diff, c.buyInterval)
	}

	if c.buySize < c.sellSize {
		return fmt.Errorf("buy size cannot be lesser than sell size")
	}
	return nil
}

func (c *Add) buySellPoints() [][2]*point.Point {
	var points [][2]*point.Point
	for price := c.beginPriceRange; price < c.endPriceRange; price += c.buyInterval {
		buy := &point.Point{
			Price:  decimal.NewFromFloat(price),
			Size:   decimal.NewFromFloat(c.buySize),
			Cancel: decimal.NewFromFloat(price + c.buyCancelOffset),
		}
		if err := buy.Check(); err != nil || buy.Side() != "BUY" {
			log.Fatal(err)
		}
		sell := &point.Point{
			Price:  decimal.NewFromFloat(price + c.sellMargin),
			Size:   decimal.NewFromFloat(c.sellSize),
			Cancel: decimal.NewFromFloat(price + c.sellMargin - c.sellCancelOffset),
		}
		if err := sell.Check(); err != nil || sell.Side() != "SELL" {
			log.Fatal(err)
		}
		points = append(points, [2]*point.Point{buy, sell})
	}
	return points
}

func (c *Add) Run(ctx context.Context, args []string) error {
	if len(args) != 0 {
		return fmt.Errorf("this command takes no arguments")
	}
	if err := c.check(); err != nil {
		return err
	}

	points := c.buySellPoints()
	if points == nil {
		return fmt.Errorf("could not determine buy/sell points")
	}

	if c.dryRun {
		for i, p := range points {
			d0, _ := json.Marshal(p[0])
			fmt.Printf("buy-%d:  %s\n", i, d0)
			d1, _ := json.Marshal(p[1])
			fmt.Printf("sell-%d: %s\n", i, d1)
		}
		return nil
	}

	req := &api.WallRequest{
		Product:       c.product,
		BuySellPoints: points,
	}
	resp, err := subcmds.Post[api.WallResponse](ctx, &c.ClientFlags, "/trader/wall", req)
	if err != nil {
		return err
	}
	jsdata, _ := json.MarshalIndent(resp, "", "  ")
	fmt.Printf("%s\n", jsdata)
	return nil
}

// func (c *Add) Command() (*flag.FlagSet, cli.CmdFunc) {
// 	if c.fset == nil {
// 		c.fset = flag.NewFlagSet("add", flag.ContinueOnError)
// 		c.ClientFlags.SetFlags(c.fset)
// 		c.fset.BoolVar(&c.dryRun, "dry-run", false, "when true only prints the trade points")
// 		c.fset.StringVar(&c.product, "product", "BCH-USD", "product id for the trade")
// 		c.fset.Float64Var(&c.beginPriceRange, "begin-price", 0, "begin price for the trading price range")
// 		c.fset.Float64Var(&c.endPriceRange, "end-price", 0, "end price for the trading price range")
// 		c.fset.Float64Var(&c.buyInterval, "buy-interval", 0, "interval between successive buy price points")
// 		c.fset.Float64Var(&c.sellMargin, "sell-margin", 0, "interval between buy and sell price points")
// 		c.fset.Float64Var(&c.buySize, "buy-size", 0, "asset buy-size for the trade")
// 		c.fset.Float64Var(&c.sellSize, "sell-size", 0, "asset sell-size for the trade")
// 		c.fset.Float64Var(&c.buyCancelOffset, "buy-cancel-offset", 50, "asset buy-cancel-at price-offset for the trade")
// 		c.fset.Float64Var(&c.sellCancelOffset, "sell-cancel-offset", 50, "asset sell-cancel-at price-offset for the trade")
// 	}
// 	return c.fset, cli.CmdFunc(c.Run)
// }

func (c *Add) Command() (*flag.FlagSet, cli.CmdFunc) {
	fset := flag.NewFlagSet("add", flag.ContinueOnError)
	c.ClientFlags.SetFlags(fset)
	fset.BoolVar(&c.dryRun, "dry-run", false, "when true only prints the trade points")
	fset.StringVar(&c.product, "product", "BCH-USD", "product id for the trade")
	fset.Float64Var(&c.beginPriceRange, "begin-price", 0, "begin price for the trading price range")
	fset.Float64Var(&c.endPriceRange, "end-price", 0, "end price for the trading price range")
	fset.Float64Var(&c.buyInterval, "buy-interval", 0, "interval between successive buy price points")
	fset.Float64Var(&c.sellMargin, "sell-margin", 0, "interval between buy and sell price points")
	fset.Float64Var(&c.buySize, "buy-size", 0, "asset buy-size for the trade")
	fset.Float64Var(&c.sellSize, "sell-size", 0, "asset sell-size for the trade")
	fset.Float64Var(&c.buyCancelOffset, "buy-cancel-offset", 50, "asset buy-cancel-at price-offset for the trade")
	fset.Float64Var(&c.sellCancelOffset, "sell-cancel-offset", 50, "asset sell-cancel-at price-offset for the trade")
	return fset, cli.CmdFunc(c.Run)
}
