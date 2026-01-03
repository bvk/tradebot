// Copyright (c) 2024 BVK Chaitanya

package coinbase

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/bvk/tradebot/coinbase/advanced"
	"github.com/bvk/tradebot/server"
	"github.com/visvasity/cli"
)

type GetOrder struct {
	secretsPath string
}

func (c *GetOrder) Command() (string, *flag.FlagSet, cli.CmdFunc) {
	fset := flag.NewFlagSet("get-order", flag.ContinueOnError)
	fset.StringVar(&c.secretsPath, "secrets-file", "", "path to credentials file")
	return "get-order", fset, cli.CmdFunc(c.run)
}

func (c *GetOrder) Purpose() string {
	return "Fetch one or more orders by their server uuid from Coinbase."
}

func (c *GetOrder) run(ctx context.Context, args []string) error {
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	if len(args) == 0 {
		return fmt.Errorf("no order id arguments")
	}

	if len(c.secretsPath) == 0 {
		return fmt.Errorf("secrets file must be specified")
	}
	secrets, err := server.SecretsFromFile(c.secretsPath)
	if err != nil {
		return err
	}
	if secrets.Coinbase == nil {
		return fmt.Errorf("secrets file has no coinbase credentials")
	}

	opts := &advanced.Options{
		HttpClientTimeout: 10 * time.Minute,
	}
	client, err := advanced.New(ctx, secrets.Coinbase.KID, secrets.Coinbase.PEM, opts)
	if err != nil {
		return fmt.Errorf("could not create coinbase client: %w", err)
	}
	defer client.Close()

	for ii, id := range args {
		order, err := client.GetOrder(ctx, id)
		if err != nil {
			return err
		}
		js, _ := json.MarshalIndent(order, "", "  ")
		fmt.Printf("%d: %s\n", ii, js)
	}

	return nil

}
