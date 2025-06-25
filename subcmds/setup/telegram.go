// Copyright (c) 2025 BVK Chaitanya

package setup

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/bvk/tradebot/ctxutil"
	"github.com/bvk/tradebot/server"
	"github.com/bvk/tradebot/telegram"
	"github.com/bvkgo/kv/kvmemdb"
	"github.com/visvasity/cli"
	"golang.org/x/term"
)

type Telegram struct {
	dataDir     string
	skipTesting bool

	ownerID  string
	adminID  string
	botToken string
}

func (c *Telegram) Purpose() string {
	return "Setup configures Telegram service API parameters"
}

func (c *Telegram) Command() (string, *flag.FlagSet, cli.CmdFunc) {
	fset := flag.NewFlagSet("telegram", flag.ContinueOnError)
	fset.StringVar(&c.dataDir, "data-dir", "", "path to the data directory")
	fset.StringVar(&c.ownerID, "owner-id", "", "Owner's telegram user id")
	fset.StringVar(&c.adminID, "admin-id", "", "Administrator's telegram user id")
	fset.StringVar(&c.botToken, "bot-token", "", "Telegram bot's authentication token")
	fset.BoolVar(&c.skipTesting, "skip-testing", false, "don't test the parameters")
	return "telegram", fset, cli.CmdFunc(c.run)
}

func (c *Telegram) Description() string {
	return `

Command "telegram" helps users configure notifications to their Telegram
account through a Telegram bot.

Telegram configuration is optional. This is only required to receive
notifications to the mobile phones. They can be configured as follows:

  $ tradebot setup telegram --owner-id=username --bot-token=USCJS2...TVP4KV

`
}

func (c *Telegram) run(ctx context.Context, args []string) error {
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

	secrets.Telegram = &telegram.Secrets{
		OwnerID:  c.ownerID,
		AdminID:  c.adminID,
		BotToken: c.botToken,
	}
	if err := secrets.Check(); err != nil {
		return err
	}

	if !c.skipTesting {
		func() {
			fmt.Println("Start a chat with telegram bot and then press any key")
			// switch stdin into 'raw' mode
			oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
			if err != nil {
				log.Fatal(err)
			}
			defer term.Restore(int(os.Stdin.Fd()), oldState)

			b := make([]byte, 1)
			_, err = os.Stdin.Read(b)
			if err != nil {
				log.Fatal(err)
			}
		}()

		// Attempt to authenticate with pushover to validate the keys.
		client, err := telegram.New(ctx, kvmemdb.New(), secrets.Telegram)
		if err != nil {
			return err
		}
		ctxutil.Sleep(ctx, time.Second)
		if err := client.SendMessage(ctx, time.Now(), "Test message from Telegram config setup; please ignore."); err != nil {
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
