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
	"syscall"

	"github.com/bvk/tradebot/etrade"
	"github.com/bvk/tradebot/server"
	"github.com/bvk/tradebot/subcmds/defaults"
	"github.com/visvasity/cli"
)

type GetPortfolio struct {
	secretsPath string
	accountID   string
}

func (c *GetPortfolio) Command() (string, *flag.FlagSet, cli.CmdFunc) {
	fset := flag.NewFlagSet("get-portfolio", flag.ContinueOnError)
	fset.StringVar(&c.secretsPath, "secrets-file", filepath.Join(defaults.DataDir(), "secrets.json"), "path to secrets.json file")
	fset.StringVar(&c.accountID, "account-id", "", "E*TRADE account ID (picked automatically if there is only one account)")
	return "get-portfolio", fset, cli.CmdFunc(c.run)
}

func (c *GetPortfolio) Purpose() string {
	return "Prints an E*TRADE account portfolio."
}

func (c *GetPortfolio) run(ctx context.Context, args []string) error {
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

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

	portfolio, err := client.GetPortfolio(ctx, c.accountID)
	if err != nil {
		return fmt.Errorf("could not get portfolio: %w", err)
	}

	js, _ := json.MarshalIndent(portfolio, "", "  ")
	fmt.Printf("%s\n", js)
	return nil
}
