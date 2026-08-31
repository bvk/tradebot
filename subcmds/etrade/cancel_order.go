// Copyright (c) 2026 Deepak Vankadaru

package etrade

import (
	"context"
	"errors"
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

type CancelOrder struct {
	secretsPath string
	orderID     int64
	accountID   string
}

func (c *CancelOrder) Command() (string, *flag.FlagSet, cli.CmdFunc) {
	fset := flag.NewFlagSet("cancel-order", flag.ContinueOnError)
	fset.StringVar(&c.secretsPath, "secrets-file", filepath.Join(defaults.DataDir(), "secrets.json"), "path to secrets.json file")
	fset.StringVar(&c.accountID, "account-id", "", "E*TRADE account ID (picked automatically if there is only one account)")
	fset.Int64Var(&c.orderID, "order-id", 0, "E*TRADE order ID")
	return "cancel-order", fset, cli.CmdFunc(c.run)
}

func (c *CancelOrder) Purpose() string {
	return "Cancel an open order on E*TRADE."
}

func (c *CancelOrder) run(ctx context.Context, args []string) error {
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
	if len(secrets.ETrade.Accounts) == 0 {
		return fmt.Errorf("etrade has no trading accounts")
	}
	if c.accountID == "" {
		if len(secrets.ETrade.Accounts) != 1 {
			return fmt.Errorf("etrade account id must be specified; it has multiple accounts")
		}
		c.accountID = secrets.ETrade.Accounts[0].AccountID
	}

	opts := etrade.CommandOptions()
	opts.Sandbox = secrets.ETrade.Sandbox
	client, err := etrade.New(ctx, secrets.ETrade, opts)
	if err != nil {
		return fmt.Errorf("could not create etrade client: %w", err)
	}
	defer client.Close()

	if err := client.CancelOrder(ctx, c.accountID, c.orderID); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("order %d not found (may already be executed or cancelled)", c.orderID)
		}
		return fmt.Errorf("could not cancel order %d: %w", c.orderID, err)
	}

	fmt.Printf("order %d cancelled\n", c.orderID)
	return nil
}
