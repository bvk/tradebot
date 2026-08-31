// Copyright (c) 2026 BVK Chaitanya

package etrade

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/bvk/tradebot/etrade/autologin"
	"github.com/bvk/tradebot/server"
	"github.com/bvk/tradebot/subcmds/defaults"
	"github.com/visvasity/cli"
)

type ListAccounts struct {
	secretsPath  string
	accountIDKey string
}

func (c *ListAccounts) Command() (string, *flag.FlagSet, cli.CmdFunc) {
	fset := flag.NewFlagSet("list-accounts", flag.ContinueOnError)
	fset.StringVar(&c.secretsPath, "secrets-file", filepath.Join(defaults.DataDir(), "secrets.json"), "path to secrets.json file")
	return "list-accounts", fset, cli.CmdFunc(c.run)
}

func (c *ListAccounts) Purpose() string {
	return "List trading accounts from E*TRADE."
}

func (c *ListAccounts) run(ctx context.Context, args []string) error {
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

	accounts, err := autologin.OAuthListAccounts(ctx, secrets.ETrade, secrets.ETrade.Sandbox)
	if err != nil {
		return fmt.Errorf("could not list accounts: %w", err)
	}
	for i, a := range accounts {
		fmt.Printf("  %d: NAME=%q ID=%s KEY=%s STATUS=%s TYPE=%s\n", i+1, a.AccountName, a.AccountID, a.AccountIDKey, a.Status, a.AccountType)
	}
	return nil
}
