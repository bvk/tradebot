// Copyright (c) 2026 BVK Chaitanya

package etrade

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/bvk/tradebot/etrade"
	"github.com/bvk/tradebot/server"
	"github.com/bvk/tradebot/subcmds/defaults"
	"github.com/visvasity/cli"
)

type GetQuote struct {
	secretsPath string
	orderID     int64
	accountID   string
}

func (c *GetQuote) Command() (string, *flag.FlagSet, cli.CmdFunc) {
	fset := flag.NewFlagSet("get-quote", flag.ContinueOnError)
	fset.StringVar(&c.secretsPath, "secrets-file", filepath.Join(defaults.DataDir(), "secrets.json"), "path to secrets.json file")
	return "get-quote", fset, cli.CmdFunc(c.run)
}

func (c *GetQuote) Purpose() string {
	return "Fetch and print a symbol information from E*TRADE."
}

func (c *GetQuote) run(ctx context.Context, args []string) error {
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	if len(args) == 0 {
		return fmt.Errorf("one or more symbol arguments are required")
	}

	if c.secretsPath == "" {
		return fmt.Errorf("--secrets-file flag is required")
	}
	secrets, err := server.SecretsFromFile(c.secretsPath)
	if err != nil {
		return err
	}
	if secrets.ETrade == nil {
		return fmt.Errorf("secrets file has no etrade credentials")
	}

	opts := etrade.CommandOptions()
	opts.Sandbox = secrets.ETrade.Sandbox
	client, err := etrade.New(ctx, secrets.ETrade, opts)
	if err != nil {
		return fmt.Errorf("could not create etrade client: %w", err)
	}
	defer client.Close()

	quotes, err := client.GetQuotes(ctx, args)
	if err != nil {
		return fmt.Errorf("could not get quotes %v: %w", args, err)
	}

	js, _ := json.MarshalIndent(quotes, "", "  ")
	fmt.Printf("%s\n", js)
	return nil
}
