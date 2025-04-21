// Copyright (c) 2023 BVK Chaitanya

package fix

import (
	"context"
	"flag"
	"fmt"
	"strings"

	"github.com/bvk/tradebot/namer"
	"github.com/bvk/tradebot/subcmds/cmdutil"
	"github.com/bvk/tradebot/waller"
	"github.com/bvkgo/kv"
	"github.com/shopspring/decimal"
	"github.com/visvasity/cli"
)

type CancelOffset struct {
	cmdutil.DBFlags

	cancelOffset float64
}

func (c *CancelOffset) Run(ctx context.Context, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("command takes one waller job-name argument")
	}
	jobArg := args[0]

	if c.cancelOffset <= 0 {
		return fmt.Errorf("a positive cancel-offset is required")
	}
	if jobArg == "" {
		return fmt.Errorf("job argument cannot be empty")
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
		w, err := waller.Load(ctx, uid, rw)
		if err != nil {
			return fmt.Errorf("could not load waller job %q: %w", uid, err)
		}
		if err := waller.FixCancelOffset(ctx, w, decimal.NewFromFloat(c.cancelOffset)); err != nil {
			return fmt.Errorf("could not fix cancel offset: %w", err)
		}
		if err := w.Save(ctx, rw); err != nil {
			return fmt.Errorf("could not save fixes: %w", err)
		}
		return nil
	}
	if err := kv.WithReadWriter(ctx, db, fixer); err != nil {
		return fmt.Errorf("could not adjust cancel-offset to job %q: %w", jobArg, err)
	}
	return nil
}

func (c *CancelOffset) Command() (string, *flag.FlagSet, cli.CmdFunc) {
	fset := new(flag.FlagSet)
	c.DBFlags.SetFlags(fset)
	fset.Float64Var(&c.cancelOffset, "cancel-offset", 0, "cancel-at price offset for the buy/sell points")
	return "cancel-offset", fset, cli.CmdFunc(c.Run)
}

func (c *CancelOffset) Purpose() string {
	return "Adjust cancel-offset price for points in a (unloaded) waller job"
}
