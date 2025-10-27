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

type GetOrder struct {
	secretsPath string

	market string
}

func (c *GetOrder) Command() (string, *flag.FlagSet, cli.CmdFunc) {
	fset := flag.NewFlagSet("get-order", flag.ContinueOnError)
	fset.StringVar(&c.market, "market", "", "market name for the orders")
	fset.StringVar(&c.secretsPath, "secrets-file", "", "path to credentials file")
	return "get-order", fset, cli.CmdFunc(c.run)
}

func (c *GetOrder) Purpose() string {
	return "Fetch one or more orders by their server assigned ids from CoinEx."
}

func (c *GetOrder) run(ctx context.Context, args []string) error {
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	if len(args) == 0 {
		return fmt.Errorf("no order id arguments")
	}
	if len(c.market) == 0 {
		return fmt.Errorf("market name is required")
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

	items, err := cex.BatchQueryOrders(ctx, c.market, ids)
	if err != nil {
		return err
	}

	for i, item := range items {
		if item.Code == 0 {
			js, _ := json.MarshalIndent(item.Data, "", "  ")
			fmt.Printf("%d: %s\n", ids[i], js)
			continue
		}
		fmt.Printf("%d: %s (%d)\n", ids[i], item.Message, item.Code)
	}
	return nil
}
