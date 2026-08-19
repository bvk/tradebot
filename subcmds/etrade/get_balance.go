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

type GetBalance struct {
	secretsPath string
	orderID     int64
	accountID   string
}

func (c *GetBalance) Command() (string, *flag.FlagSet, cli.CmdFunc) {
	fset := new(flag.FlagSet)
	fset.StringVar(&c.secretsPath, "secrets-file", filepath.Join(defaults.DataDir(), "secrets.json"), "path to secrets.json file")
	fset.StringVar(&c.accountID, "account-id", "", "E*TRADE account ID (picked automatically if there is only one account)")
	return "get-balance", fset, cli.CmdFunc(c.run)
}

func (c *GetBalance) Purpose() string {
	return "Prints the account balance from E*TRADE."
}

func (c *GetBalance) run(ctx context.Context, args []string) error {
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

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

	balance, err := client.GetBalance(ctx, c.accountID)
	if err != nil {
		return fmt.Errorf("could not get order %d: %w", c.orderID, err)
	}

	js, _ := json.MarshalIndent(balance, "", "  ")
	fmt.Printf("%s\n", js)
	return nil
}
