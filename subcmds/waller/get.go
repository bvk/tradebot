// Copyright (c) 2023 BVK Chaitanya

package waller

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"

	"github.com/bvk/tradebot/cli"
	"github.com/bvk/tradebot/gobs"
	"github.com/bvk/tradebot/kvutil"
	"github.com/bvk/tradebot/subcmds/cmdutil"
	"github.com/bvkgo/kv"
)

type Get struct {
	cmdutil.DBFlags
}

func (c *Get) Run(ctx context.Context, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("this command takes one (key) argument")
	}

	getter := func(ctx context.Context, r kv.Reader) error {
		gv, err := kvutil.Get[gobs.WallerState](ctx, r, args[0])
		if err != nil {
			return err
		}

		d, _ := json.MarshalIndent(gv, "", "  ")
		fmt.Printf("%s\n", d)
		return nil
	}

	db, err := c.DBFlags.GetDatabase(ctx)
	if err != nil {
		return err
	}
	if err := kv.WithReader(ctx, db, getter); err != nil {
		return err
	}
	return nil
}

func (c *Get) Command() (*flag.FlagSet, cli.CmdFunc) {
	fset := flag.NewFlagSet("get", flag.ContinueOnError)
	c.DBFlags.SetFlags(fset)
	return fset, cli.CmdFunc(c.Run)
}

func (c *Get) Synopsis() string {
	return "Prints a waller state from a key"
}
