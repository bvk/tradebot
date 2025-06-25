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
	"strings"

	"github.com/bvk/tradebot/coinbase"
	"github.com/bvk/tradebot/server"
	"github.com/bvkgo/kv/kvmemdb"
	"github.com/visvasity/cli"
)

type Coinbase struct {
	dataDir     string
	skipTesting bool
	key         string
	pem         string
}

func (c *Coinbase) Purpose() string {
	return "Setup configures Coinbase API access parameters"
}

func (c *Coinbase) Command() (string, *flag.FlagSet, cli.CmdFunc) {
	fset := flag.NewFlagSet("coinbase", flag.ContinueOnError)
	fset.StringVar(&c.dataDir, "data-dir", "", "path to the data directory")
	fset.StringVar(&c.key, "key", "", "Coinbase API Key ID as a string")
	fset.StringVar(&c.pem, "pem", "", "Coinbase API Private Key as a string")
	fset.BoolVar(&c.skipTesting, "skip-testing", false, "don't test the parameters")
	return "coinbase", fset, cli.CmdFunc(c.run)
}

func (c *Coinbase) Description() string {
	return `

Command "coinbase" helps users configure Coinbase API keys.

Coinbase API keys are required to query and put buy/sell orders on the
coinbase. They can be configured as follows:

  $ tradebot setup coinbase --key=organizations/org-uuid/apiKeys/key-uuid --pem="-----BEGIN EC PRIVATE ... PRIVATE KEY-----\n"

`
}

func (c *Coinbase) run(ctx context.Context, args []string) error {
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
		return fmt.Errorf("--key flag is required")
	}
	if len(c.pem) == 0 {
		return fmt.Errorf("--pem flag is required")
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
	c.pem = strings.ReplaceAll(c.pem, `\\n`, "\n")
	c.pem = strings.ReplaceAll(c.pem, `\n`, "\n")
	secrets.Coinbase = &coinbase.Credentials{
		KID: c.key,
		PEM: c.pem,
	}
	if !c.skipTesting {
		// Attempt to authenticate with coinbase to validate the keys.
		client, err := coinbase.New(ctx, kvmemdb.New(), c.key, c.pem, coinbase.SubcommandOptions())
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
