// Copyright (c) 2025 BVK Chaitanya

package coinex

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/bvk/tradebot/coinex"
	"github.com/bvk/tradebot/server"
	"github.com/visvasity/cli"
)

type GetBalances struct {
	secretsPath string

	market string
}

func (c *GetBalances) Command() (string, *flag.FlagSet, cli.CmdFunc) {
	fset := flag.NewFlagSet("get-balances", flag.ContinueOnError)
	fset.StringVar(&c.market, "market", "", "market name for the orders")
	fset.StringVar(&c.secretsPath, "secrets-file", "", "path to credentials file")
	return "get-balances", fset, cli.CmdFunc(c.run)
}

func (c *GetBalances) Purpose() string {
	return "Print asset balances from CoinEx."
}

func (c *GetBalances) run(ctx context.Context, args []string) error {
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

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

	opts := &coinex.Options{
		NoWebsocket: true,
	}
	cex, err := coinex.New(ctx, secrets.CoinEx.Key, secrets.CoinEx.Secret, opts)
	if err != nil {
		return fmt.Errorf("could not create coinex client: %w", err)
	}
	defer cex.Close()

	ids := make([]int64, len(args))
	for i, arg := range args {
		id, err := strconv.ParseInt(arg, 10, 64)
		if err != nil {
			return err
		}
		ids[i] = id
	}

	balances, err := cex.GetBalances(ctx)
	if err != nil {
		return err
	}

	js, _ := json.MarshalIndent(balances, "", "  ")
	fmt.Printf("%s\n", js)
	return nil
}
