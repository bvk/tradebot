// Copyright (c) 2025 BVK Chaitanya

package fix

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/bvk/tradebot/namer"
	"github.com/bvk/tradebot/subcmds/cmdutil"
	"github.com/bvk/tradebot/waller"
	"github.com/bvkgo/kv"
	"github.com/visvasity/cli"
)

type SwitchToUSD struct {
	cmdutil.DBFlags
}

func (c *SwitchToUSD) Purpose() string {
	return "Converts *-USDC waller jobs to *-USD instead"
}

func (c *SwitchToUSD) Description() string {
	return `

This command allows users to convert their waller jobs using USDC as the base
currency (for example, BCH-USDC, BTC-USDC, etc.) into their USD counter parts
(BCH-USD, BTC-USD, etc.).

The buy and sell prices for the trade points is unchanged, only the target
product is updated.

Users MUST ensure that tradebot job is *NOT* running when they run this
command.

`
}

func (c *SwitchToUSD) Command() (string, *flag.FlagSet, cli.CmdFunc) {
	fset := new(flag.FlagSet)
	c.DBFlags.SetFlags(fset)
	return "switch-to-usd", fset, cli.CmdFunc(c.Run)
}

func (c *SwitchToUSD) Run(ctx context.Context, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("command takes one waller job-name argument")
	}
	jobArg := args[0]
	if jobArg == "" {
		return fmt.Errorf("job argument cannot be empty")
	}
	if c.DBFlags.IsRemoteDatabase() {
		return fmt.Errorf("this command should not be used with remote databases")
	}

	db, closer, err := c.DBFlags.GetDatabase(ctx)
	if err != nil {
		return fmt.Errorf("could not create db instance: %w", err)
	}
	defer closer()

	fixer := func(ctx context.Context, rw kv.ReadWriter) error {
		_, uid, typename, err := namer.Resolve(ctx, rw, jobArg)
		if err != nil {
			return fmt.Errorf("could not resolve job argument %q: %w", jobArg, err)
		}
		if !strings.EqualFold(typename, "Waller") {
			return fmt.Errorf("this fix is only meant for waller jobs")
		}
		if err := getBotIsNotRunningConfirmation(ctx); err != nil {
			return err
		}
		w, err := waller.Load(ctx, uid, rw)
		if err != nil {
			return fmt.Errorf("could not load waller job %q: %w", uid, err)
		}
		if err := waller.SwitchToUSD(ctx, w); err != nil {
			return fmt.Errorf("could not convert to USD: %w", err)
		}
		if err := w.Save(ctx, rw); err != nil {
			return fmt.Errorf("could not save updated job: %w", err)
		}
		return nil
	}
	if err := kv.WithReadWriter(ctx, db, fixer); err != nil {
		return fmt.Errorf("could not convert %q to an USD job: %w", jobArg, err)
	}
	return nil
}

func getBotIsNotRunningConfirmation(ctx context.Context) error {
	fmt.Println("NOTE: This command requires tradebot is shutdown and is NOT RUNNING.")
	fmt.Println()
	fmt.Printf("Did you verify that tradebot is NOT running? (yes/no): ")
	var isRunning string
	fmt.Scanf("%s", &isRunning)
	if !strings.EqualFold(isRunning, "yes") {
		return fmt.Errorf("could not confirm bot is not running: %w", os.ErrInvalid)
	}
	return nil
}
