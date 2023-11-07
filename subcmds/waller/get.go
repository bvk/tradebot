// Copyright (c) 2023 BVK Chaitanya

package waller

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"

	"github.com/bvk/tradebot/cli"
	"github.com/bvk/tradebot/kvutil"
	"github.com/bvk/tradebot/subcmds/db"
	"github.com/bvk/tradebot/waller"
	"github.com/bvkgo/kv"
)

type Get struct {
	db.Flags
}

func (c *Get) Run(ctx context.Context, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("this command takes one (key) argument")
	}

	getter := func(ctx context.Context, r kv.Reader) error {
		gv, err := kvutil.Get[waller.State](ctx, r, args[0])
		if err != nil {
			return err
		}

		d, _ := json.Marshal(gv)
		fmt.Printf("%s\n", d)
		return nil
	}

	db, err := c.Flags.GetDatabase()
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
	c.Flags.SetFlags(fset)
	return fset, cli.CmdFunc(c.Run)
}

func (c *Get) Synopsis() string {
	return "Prints a single waller job info from a key"
}
