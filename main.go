// Copyright (c) 2023 BVK Chaitanya

package main

import (
	"context"
	"log"
	"log/slog"
	"net"
	"os"
	"runtime/debug"

	"github.com/bvk/tradebot/envfile"
	"github.com/bvk/tradebot/subcmds"
	"github.com/bvk/tradebot/subcmds/coinbase"
	"github.com/bvk/tradebot/subcmds/coinex"
	"github.com/bvk/tradebot/subcmds/configure/alerts"
	"github.com/bvk/tradebot/subcmds/db"
	"github.com/bvk/tradebot/subcmds/exchange"
	"github.com/bvk/tradebot/subcmds/fix"
	"github.com/bvk/tradebot/subcmds/job"
	"github.com/bvk/tradebot/subcmds/limiter"
	"github.com/bvk/tradebot/subcmds/looper"
	"github.com/bvk/tradebot/subcmds/setup"
	"github.com/bvk/tradebot/subcmds/waller"
	"github.com/bvk/tradebot/subcmds/watcher"
	"github.com/visvasity/cli"
)

func init() {
	// Force use Google DNS instead of the system dns.
	net.DefaultResolver = &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, network, "8.8.8.8:53")
		},
	}
}

func main() {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("CAUGHT PANIC", "panic", r)
			slog.Error(string(debug.Stack()))
			panic(r)
		}
	}()

	if err := envfile.UpdateEnv(".tradebotenv", envfile.VariableNamePrefix("TRADEBOT_")); err != nil {
		log.Fatal(err)
	}

	dbCmds := []cli.Command{
		new(db.Get),
		new(db.Set),
		new(db.Edit),
		new(db.Delete),
		new(db.List),
		new(db.Backup),
		new(db.Restore),
	}

	setupCmds := []cli.Command{
		new(setup.Coinbase),
		new(setup.CoinEx),
		new(setup.PushOver),
		new(setup.Telegram),
	}

	alertsCmds := []cli.Command{
		new(alerts.LowBalanceLimits),
	}

	configureCmds := []cli.Command{
		cli.NewGroup("alerts", "Configure Alerts", alertsCmds...),
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

	watcherCmds := []cli.Command{
		new(watcher.Add),
	}

	exchangeCmds := []cli.Command{
		new(exchange.GetOrder),
		new(exchange.GetProduct),
		new(exchange.UpdateProduct),
	}

	coinbaseCmds := []cli.Command{
		new(coinbase.Sync),
		new(coinbase.List),
		new(coinbase.GetOrder),
	}

	coinexCmds := []cli.Command{
		new(coinex.GetPrice),
		new(coinex.GetOrder),
		new(coinex.FixFee),
		new(coinex.RunTest),
	}

	cmds := []cli.Command{
		new(subcmds.Run),
		new(subcmds.Status),
		cli.NewGroup("configure", "Updates runtime configuration", configureCmds...),
		cli.NewGroup("fix", "Fix misc. metadata issues", fixCmds...),
		cli.NewGroup("job", "Control trader jobs", jobCmds...),
		cli.NewGroup("db", "View/update database directly", dbCmds...),
		cli.NewGroup("limiter", "Manage limit buys/sells", limiterCmds...),
		cli.NewGroup("looper", "Manage buy-sell loops", looperCmds...),
		cli.NewGroup("waller", "Manage trades in a price range", wallerCmds...),
		cli.NewGroup("watcher", "Simulate trades in a price range", watcherCmds...),
		cli.NewGroup("exchange", "View/query exchange directly", exchangeCmds...),
		cli.NewGroup("coinbase", "Coinbase exchange operations", coinbaseCmds...),
		cli.NewGroup("coinex", "CoinEx exchange operations", coinexCmds...),
		cli.NewGroup("setup", "Setup operations", setupCmds...),
	}
	if err := cli.Run(context.Background(), cmds, os.Args[1:]); err != nil {
		log.Fatal(err)
	}
}
