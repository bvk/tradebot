// Copyright (c) 2023 BVK Chaitanya

package limiter

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"

	"github.com/bvkgo/kv"
	"github.com/bvkgo/tradebot/cli"
	"github.com/bvkgo/tradebot/kvutil"
	"github.com/bvkgo/tradebot/limiter"
	"github.com/bvkgo/tradebot/subcmds/db"
)

type Get struct {
	db.Flags
}

func (c *Get) Run(ctx context.Context, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("this command takes one (key) argument")
	}

	getter := func(ctx context.Context, r kv.Reader) error {
		gv, err := kvutil.Get[limiter.State](ctx, r, args[0])
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
	return "Prints a single limit buy/sell job info from a key"
}
