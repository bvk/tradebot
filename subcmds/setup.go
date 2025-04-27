// Copyright (c) 2025 BVK Chaitanya

package subcmds

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/bvk/tradebot/cli"
	"github.com/bvk/tradebot/coinbase"
	"github.com/bvk/tradebot/pushover"
	"github.com/bvk/tradebot/server"
	"github.com/bvkgo/kv/kvmemdb"
)

type Setup struct {
	dataDir     string
	secretsPath string
	skipTesting bool
}

func (c *Setup) Synopsis() string {
	return "Setup prints and/or configures tradebot daemon"
}

func (c *Setup) Command() (*flag.FlagSet, cli.CmdFunc) {
	fset := flag.NewFlagSet("setup", flag.ContinueOnError)
	fset.StringVar(&c.dataDir, "data-dir", "", "path to the data directory")
	fset.StringVar(&c.secretsPath, "secrets-file", "", "path to credentials file")
	fset.BoolVar(&c.skipTesting, "skip-testing", false, "don't test the parameters")
	return fset, cli.CmdFunc(c.run)
}

func (c *Setup) CommandHelp() string {
	return `

Command "setup" helps users configure Coinbase API keys and notification keys
for the Pushover service. Command prints current config when run without any
arguments.

COINBASE PARAMETERS

Coinbase API keys are required to query and put buy/sell orders on the
coinbase. They can be configured as follows:

  $ tradebot setup coinbase-key=organizations/org-uuid/apiKeys/key-uuid coinbase-pem="-----BEGIN EC PRIVATE ... PRIVATE KEY-----\n"

PUSHOVER PARAMETERS

Pushover keys are optional. They are required to receive notifications to the
mobile phones. They can be configured as follows:

  $ tradebot setup pushover-app=awja5ue...ito7svf pushover-user=uscjs2...tvp4kv
`
}

func (c *Setup) run(ctx context.Context, args []string) error {
	if len(c.dataDir) == 0 {
		c.dataDir = filepath.Join(os.Getenv("HOME"), ".tradebot")
	}
	if _, err := os.Stat(c.dataDir); err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("could not stat data directory %q: %w", c.dataDir, err)
		}
		if len(args) == 0 {
			return fmt.Errorf("tradebot is not configured")
		}
		if err := os.MkdirAll(c.dataDir, 0700); err != nil {
			return fmt.Errorf("could not create data directory %q: %w", c.dataDir, err)
		}
	}
	dataDir, err := filepath.Abs(c.dataDir)
	if err != nil {
		return fmt.Errorf("could not determine data-dir %q absolute path: %w", c.dataDir, err)
	}

	if len(c.secretsPath) == 0 {
		c.secretsPath = filepath.Join(dataDir, "secrets.json")
	}
	secrets, err := server.SecretsFromFile(c.secretsPath)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if len(args) == 0 {
			return fmt.Errorf("tradebot is not configured")
		}
	}

	if len(args) == 0 {
		js, _ := json.MarshalIndent(secrets, "", "  ")
		fmt.Printf("%s\n", js)
		return nil
	}

	if secrets == nil {
		secrets = &server.Secrets{}
	}

	validKeys := []string{"coinbase-key", "coinbase-pem", "pushover-app", "pushover-user"}
	kvMap := make(map[string]string)
	// Parse config values from the command-line.
	for _, arg := range args {
		before, after, found := strings.Cut(arg, "=")
		if !found {
			return fmt.Errorf("invalid config argument %q", arg)
		}
		if !slices.Contains(validKeys, before) {
			return fmt.Errorf("invalid/unrecognized config item key %q", before)
		}
		if v, ok := kvMap[before]; ok && v != after {
			return fmt.Errorf("config item key %q is found with different values", before)
		}
		kvMap[before] = after
	}

	coinbaseKey := kvMap["coinbase-key"]
	coinbasePem := kvMap["coinbase-pem"]
	if len(coinbaseKey) != 0 || len(coinbasePem) != 0 {
		if len(coinbaseKey) == 0 || len(coinbasePem) == 0 {
			return fmt.Errorf(`both "coinbase-key" and "coinbase-pem" parameters are required`)
		}
		// Replace escaped newline characters with newlines.
		coinbasePem = strings.ReplaceAll(coinbasePem, `\\n`, "\n")
		coinbasePem = strings.ReplaceAll(coinbasePem, `\n`, "\n")
		secrets.Coinbase = &coinbase.Credentials{
			KID: coinbaseKey,
			PEM: coinbasePem,
		}
		if !c.skipTesting {
			// Attempt to authenticate with coinbase to validate the keys.
			client, err := coinbase.New(ctx, kvmemdb.New(), coinbaseKey, coinbasePem, coinbase.SubcommandOptions())
			if err != nil {
				return err
			}
			client.Close()
		}
	}

	pushoverApp := kvMap["pushover-app"]
	pushoverUser := kvMap["pushover-user"]
	if len(pushoverUser) != 0 || len(pushoverApp) != 0 {
		if len(pushoverApp) == 0 || len(pushoverUser) == 0 {
			return fmt.Errorf(`both "pushover-app" and "pushover-user" parameters are required`)
		}
		secrets.Pushover = &pushover.Keys{
			ApplicationKey: pushoverApp,
			UserKey:        pushoverUser,
		}
		if !c.skipTesting {
			// Attempt to authenticate with pushover to validate the keys.
			client, err := pushover.New(secrets.Pushover)
			if err != nil {
				return err
			}
			if err := client.SendMessage(ctx, time.Now(), "Test message from Pushover config setup; please ignore."); err != nil {
				return err
			}
		}
	}

	js, err := json.MarshalIndent(secrets, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(c.secretsPath, js, os.FileMode(0600)); err != nil {
		return err
	}
	return nil
}
