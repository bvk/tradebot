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
	"time"

	"github.com/bvk/tradebot/pushover"
	"github.com/bvk/tradebot/server"
	"github.com/visvasity/cli"
)

type PushOver struct {
	dataDir     string
	skipTesting bool

	appID  string
	userID string
}

func (c *PushOver) Purpose() string {
	return "Setup configures PushOver service API parameters"
}

func (c *PushOver) Command() (string, *flag.FlagSet, cli.CmdFunc) {
	fset := flag.NewFlagSet("pushover", flag.ContinueOnError)
	fset.StringVar(&c.dataDir, "data-dir", "", "path to the data directory")
	fset.StringVar(&c.userID, "user-id", "", "PushOver service user identifier")
	fset.StringVar(&c.appID, "app-id", "", "PushOver service Application identifier")
	fset.BoolVar(&c.skipTesting, "skip-testing", false, "don't test the parameters")
	return "pushover", fset, cli.CmdFunc(c.run)
}

func (c *PushOver) Description() string {
	return `

Command "pushover" helps users configure notifications through the
Pushover service.

Pushover keys are optional. They are only required to receive notifications to
the mobile phones. They can be configured as follows:

  $ tradebot setup pushover --app-id=awja5ue...ito7svf --user-id=uscjs2...tvp4kv

`
}

func (c *PushOver) run(ctx context.Context, args []string) error {
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

	secrets.Pushover = &pushover.Keys{
		ApplicationKey: c.appID,
		UserKey:        c.userID,
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

	js, err := json.MarshalIndent(secrets, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(secretsPath, js, os.FileMode(0600)); err != nil {
		return err
	}
	return nil
}
