// Copyright (c) 2026 Deepak Vankadaru

package etrade

import (
	"bytes"
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

type Wget struct {
	secretsPath string
	outputPath  string
}

func (c *Wget) Command() (string, *flag.FlagSet, cli.CmdFunc) {
	fset := flag.NewFlagSet("wget", flag.ContinueOnError)
	fset.StringVar(&c.secretsPath, "secrets-file", filepath.Join(defaults.DataDir(), "secrets.json"), "path to secrets.json file")
	fset.StringVar(&c.outputPath, "output-file", "", "path to write the response (default stdout)")
	return "wget", fset, cli.CmdFunc(c.run)
}

func (c *Wget) Purpose() string {
	return "Fetch a URL using E*TRADE account OAuth credentials."
}

func (c *Wget) run(ctx context.Context, args []string) error {
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	if len(args) != 1 {
		return fmt.Errorf("exactly one URL argument is required")
	}

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

	opts := etrade.CommandOptions()
	opts.Sandbox = secrets.ETrade.Sandbox
	client, err := etrade.New(ctx, secrets.ETrade, opts)
	if err != nil {
		return fmt.Errorf("could not create etrade client: %w", err)
	}
	defer client.Close()

	data, err := client.GetURL(ctx, args[0])
	if err != nil {
		return fmt.Errorf("could not fetch %s: %w", args[0], err)
	}

	out := formatJSON(data)
	if c.outputPath == "" {
		_, err := os.Stdout.Write(out)
		return err
	}
	if err := os.WriteFile(c.outputPath, out, 0600); err != nil {
		return fmt.Errorf("could not write output file %s: %w", c.outputPath, err)
	}
	return nil
}

func formatJSON(data []byte) []byte {
	if len(bytes.TrimSpace(data)) == 0 {
		return data
	}
	var buf bytes.Buffer
	if err := json.Indent(&buf, data, "", "  "); err != nil {
		return data
	}
	buf.WriteByte('\n')
	return buf.Bytes()
}
