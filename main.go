// Copyright (c) 2023 BVK Chaitanya

package main

import (
	"context"
	"log"
	"os"

	"github.com/bvk/tradebot/cli"
	"github.com/bvk/tradebot/subcmds"
	"github.com/bvk/tradebot/subcmds/coinbase"
	"github.com/bvk/tradebot/subcmds/db"
	"github.com/bvk/tradebot/subcmds/exchange"
	"github.com/bvk/tradebot/subcmds/fix"
	"github.com/bvk/tradebot/subcmds/job"
	"github.com/bvk/tradebot/subcmds/limiter"
	"github.com/bvk/tradebot/subcmds/looper"
	"github.com/bvk/tradebot/subcmds/waller"
)

func main() {
	dbCmds := []cli.Command{
		new(db.Get),
		new(db.Set),
		new(db.Edit),
		new(db.Delete),
		new(db.List),
		new(db.Backup),
		new(db.Restore),
	}

	fixCmds := []cli.Command{
		new(fix.CancelOffset),
		new(fix.DedupLimiterIDs),
		new(fix.ResolveOrders),
	}

	jobCmds := []cli.Command{
		new(job.List),
		new(job.Pause),
		new(job.Resume),
		new(job.Cancel),
		new(job.Actions),
		new(job.Export),
		new(job.Import),
		new(job.SetName),
		new(job.SetOption),
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
		new(waller.List),
		new(waller.Get),
		new(waller.Query),
		new(waller.Upgrade),
	}

	exchangeCmds := []cli.Command{
		new(exchange.GetOrder),
		new(exchange.GetProduct),
	}

	coinbaseCmds := []cli.Command{
		new(coinbase.Sync),
		new(coinbase.List),
	}

	cmds := []cli.Command{
		new(subcmds.Run),
		new(subcmds.Status),
		new(subcmds.IDGen),
		cli.CommandGroup("fix", "Fix misc. metadata issues", fixCmds...),
		cli.CommandGroup("job", "Control trader jobs", jobCmds...),
		cli.CommandGroup("db", "View/update database directly", dbCmds...),
		cli.CommandGroup("limiter", "Manage limit buys/sells", limiterCmds...),
		cli.CommandGroup("looper", "Manage buy-sell loops", looperCmds...),
		cli.CommandGroup("waller", "Manage trades in a price range", wallerCmds...),
		cli.CommandGroup("exchange", "View/query exchange directly", exchangeCmds...),
		cli.CommandGroup("coinbase", "Handles coinbase specific operations", coinbaseCmds...),
	}
	if err := cli.Run(context.Background(), cmds, os.Args[1:]); err != nil {
		log.Fatal(err)
	}
}
