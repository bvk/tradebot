// Copyright (c) 2023 BVK Chaitanya

package main

import (
	"context"
	"log"
	"os"

	"github.com/bvkgo/tradebot/cli"
	"github.com/bvkgo/tradebot/coinbase"
	"github.com/bvkgo/tradebot/exchange"
	"github.com/bvkgo/tradebot/subcmds"
	"github.com/bvkgo/tradebot/subcmds/db"
	"github.com/bvkgo/tradebot/subcmds/limiter"
	"github.com/bvkgo/tradebot/subcmds/looper"
	"github.com/bvkgo/tradebot/subcmds/waller"
)

var _ exchange.Product = &coinbase.Product{}

func main() {
	dbCmds := []cli.Command{
		new(db.Get),
		new(db.Set),
		new(db.Delete),
		new(db.List),
	}

	limiterCmds := []cli.Command{
		new(limiter.Add),
		new(limiter.List),
		new(limiter.Get),
	}

	looperCmds := []cli.Command{
		new(looper.Add),
		new(looper.List),
		new(looper.Get),
	}

	wallerCmds := []cli.Command{
		new(waller.Add),
	}

	cmds := []cli.Command{
		new(subcmds.Run),
		cli.CommandGroup("db", "Manage db content manually", dbCmds...),
		cli.CommandGroup("limiter", "Manage limit buys/sells", limiterCmds...),
		cli.CommandGroup("looper", "Manage buy-sell loops", looperCmds...),
		cli.CommandGroup("waller", "Manage trades in a price range", wallerCmds...),
	}
	if err := cli.Run(context.Background(), cmds, os.Args[1:]); err != nil {
		log.Fatal(err)
	}
}
