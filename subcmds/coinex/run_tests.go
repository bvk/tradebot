// Copyright (c) 2025 BVK Chaitanya

package coinex

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/bvk/tradebot/coinex"
	"github.com/bvk/tradebot/server"
	"github.com/visvasity/cli"
	"github.com/visvasity/topic"
)

type RunTest struct {
	secretsPath string
}

func (c *RunTest) Command() (string, *flag.FlagSet, cli.CmdFunc) {
	fset := new(flag.FlagSet)
	fset.StringVar(&c.secretsPath, "secrets-file", "", "path to credentials file")
	return "run-test", fset, cli.CmdFunc(c.run)
}

func (c *RunTest) Purpose() string {
	return "Runs one or more tests against CoinEx exchange"
}

func (c *RunTest) run(ctx context.Context, args []string) error {
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	if len(args) == 0 {
		return fmt.Errorf("at least one argument is required")
	}
	if len(c.secretsPath) == 0 {
		return fmt.Errorf("secrets file must be specified")
	}
	secrets, err := server.SecretsFromFile(c.secretsPath)
	if err != nil {
		return err
	}
	if secrets.CoinEx == nil {
		return fmt.Errorf("secrets file has no coinex credentials")
	}

	switch args[0] {
	default:
		return fmt.Errorf("unknown test name %q", args[0])

	case "test-balance-updates":
		return c.testBalanceUpdates(ctx, secrets, args[1:])
	}
}

func (c *RunTest) testBalanceUpdates(ctx context.Context, secrets *server.Secrets, args []string) error {
	opts := &coinex.Options{}
	cex, err := coinex.NewExchange(ctx, secrets.CoinEx.Key, secrets.CoinEx.Secret, opts)
	if err != nil {
		return fmt.Errorf("could not create coinex client: %w", err)
	}
	defer cex.Close()

	updates, err := cex.GetBalanceUpdates()
	if err != nil {
		return err
	}
	defer updates.Close()

	updatesCh, err := topic.ReceiveCh(updates)
	if err != nil {
		return err
	}

	for {
		select {
		case <-ctx.Done():
			return context.Cause(ctx)
		case v := <-updatesCh:
			log.Println(v.Balance())
		}
	}
}
