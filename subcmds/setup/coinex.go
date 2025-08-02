// Copyright (c) 2025 BVK Chaitanya

package setup

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/bvk/tradebot/coinex"
	"github.com/bvk/tradebot/server"
	"github.com/visvasity/cli"
)

type CoinEx struct {
	dataDir     string
	skipTesting bool
	key         string
	secret      string
}

func (c *CoinEx) Purpose() string {
	return "Setup configures CoinEx API access parameters"
}

func (c *CoinEx) Command() (string, *flag.FlagSet, cli.CmdFunc) {
	fset := flag.NewFlagSet("coinex", flag.ContinueOnError)
	fset.StringVar(&c.dataDir, "data-dir", "", "path to the data directory")
	fset.StringVar(&c.key, "access-key", "", "CoinEx API access key as a string")
	fset.StringVar(&c.secret, "access-secret", "", "CoinEx API access secret as a string")
	fset.BoolVar(&c.skipTesting, "skip-testing", false, "don't test the parameters")
	return "coinex", fset, cli.CmdFunc(c.run)
}

func (c *CoinEx) Description() string {
	return `

Command "coinex" helps users configure CoinEx exchange API keys.

CoinEx API keys are required to query and put buy/sell orders on the CoinEx
exchange. They can be configured as follows:

  $ tradebot setup coinex --access-key=xxxx --access-secret=yyyyy

`
}

func (c *CoinEx) run(ctx context.Context, args []string) error {
	if len(c.dataDir) == 0 {
		c.dataDir = filepath.Join(os.Getenv("HOME"), ".tradebot")
	}
	if _, err := os.Stat(c.dataDir); err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("could not stat data directory %q: %w", c.dataDir, err)
		}
		if err := os.MkdirAll(c.dataDir, 0700); err != nil {
			return fmt.Errorf("could not create data directory %q: %w", c.dataDir, err)
		}
	}
	dataDir, err := filepath.Abs(c.dataDir)
	if err != nil {
		return fmt.Errorf("could not determine data-dir %q absolute path: %w", c.dataDir, err)
	}

	if len(c.key) == 0 {
		return fmt.Errorf("--access-key flag is required")
	}
	if len(c.secret) == 0 {
		return fmt.Errorf("--access-secret flag is required")
	}

	secretsPath := filepath.Join(dataDir, "secrets.json")
	secrets, err := server.SecretsFromFile(secretsPath)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}

	if secrets == nil {
		secrets = &server.Secrets{}
	}

	// Replace escaped newline characters with newlines.
	secrets.CoinEx = &coinex.Credentials{
		Key:    c.key,
		Secret: c.secret,
	}
	if !c.skipTesting {
		// Attempt to authenticate with coinex to validate the keys.
		client, err := coinex.New(ctx, c.key, c.secret, nil /* opts */)
		if err != nil {
			return err
		}
		client.Close()
	}

	js, err := json.MarshalIndent(secrets, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(secretsPath, js, os.FileMode(0600)); err != nil {
		return err
	}
	return nil
}
