// Copyright (c) 2023 BVK Chaitanya

package main

import (
	"context"
	"log"
	"os"

	"github.com/bvk/tradebot/subcmds"
	"github.com/bvk/tradebot/subcmds/coinbase"
	"github.com/bvk/tradebot/subcmds/db"
	"github.com/bvk/tradebot/subcmds/exchange"
	"github.com/bvk/tradebot/subcmds/fix"
	"github.com/bvk/tradebot/subcmds/job"
	"github.com/bvk/tradebot/subcmds/limiter"
	"github.com/bvk/tradebot/subcmds/looper"
	"github.com/bvk/tradebot/subcmds/waller"
	"github.com/visvasity/cli"
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
		new(fix.SwitchToUSD),
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
		new(waller.Simulate),
	}

	exchangeCmds := []cli.Command{
		new(exchange.GetOrder),
		new(exchange.GetProduct),
	}

	coinbaseCmds := []cli.Command{
		new(coinbase.Sync),
		new(coinbase.List),
		new(coinbase.GetOrder),
	}

	cmds := []cli.Command{
		new(subcmds.Run),
		new(subcmds.Status),
		new(subcmds.Setup),
		cli.NewGroup("fix", "Fix misc. metadata issues", fixCmds...),
		cli.NewGroup("job", "Control trader jobs", jobCmds...),
		cli.NewGroup("db", "View/update database directly", dbCmds...),
		cli.NewGroup("limiter", "Manage limit buys/sells", limiterCmds...),
		cli.NewGroup("looper", "Manage buy-sell loops", looperCmds...),
		cli.NewGroup("waller", "Manage trades in a price range", wallerCmds...),
		cli.NewGroup("exchange", "View/query exchange directly", exchangeCmds...),
		cli.NewGroup("coinbase", "Handles coinbase specific operations", coinbaseCmds...),
	}
	if err := cli.Run(context.Background(), cmds, os.Args[1:]); err != nil {
		log.Fatal(err)
	}
}
