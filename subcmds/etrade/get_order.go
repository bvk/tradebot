// Copyright (c) 2026 Deepak Vankadaru

package etrade

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"

	"github.com/bvk/tradebot/etrade"
	"github.com/bvk/tradebot/server"
	"github.com/bvk/tradebot/subcmds/defaults"
	"github.com/visvasity/cli"
)

type GetOrder struct {
	secretsPath string
	orderID     int64
}

func (c *GetOrder) Command() (string, *flag.FlagSet, cli.CmdFunc) {
	fset := flag.NewFlagSet("get-order", flag.ContinueOnError)
	fset.StringVar(&c.secretsPath, "secrets-file", filepath.Join(defaults.DataDir(), "secrets.json"), "path to secrets.json file")
	fset.Int64Var(&c.orderID, "order-id", 0, "E*TRADE order ID")
	return "get-order", fset, cli.CmdFunc(c.run)
}

func (c *GetOrder) Purpose() string {
	return "Fetch a single order by ID from E*TRADE."
}

func (c *GetOrder) run(ctx context.Context, args []string) error {
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	if c.secretsPath == "" {
		return fmt.Errorf("--secrets-file flag is required")
	}
	if c.orderID == 0 {
		// Allow passing order ID as a positional argument too.
		if len(args) == 1 {
			id, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid order ID %q: %w", args[0], err)
			}
			c.orderID = id
		} else {
			return fmt.Errorf("--order-id flag is required")
		}
	}

	secrets, err := server.SecretsFromFile(c.secretsPath)
	if err != nil {
		return err
	}
	if secrets.ETrade == nil {
		return fmt.Errorf("secrets file has no etrade credentials")
	}

	opts := &etrade.Options{Sandbox: secrets.ETrade.Sandbox}
	client, err := etrade.New(ctx, secrets.ETrade, opts)
	if err != nil {
		return fmt.Errorf("could not create etrade client: %w", err)
	}
	defer client.Close()

	order, err := client.GetOrder(ctx, c.orderID)
	if err != nil {
		return fmt.Errorf("could not get order %d: %w", c.orderID, err)
	}

	js, _ := json.MarshalIndent(order, "", "  ")
	fmt.Printf("%s\n", js)
	return nil
}
