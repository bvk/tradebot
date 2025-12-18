// Copyright (c) 2025 Deepak Vankadaru

package limiter

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path"

	"github.com/bvk/tradebot/gobs"
	"github.com/bvk/tradebot/kvutil"
	"github.com/bvk/tradebot/limiter"
	"github.com/bvk/tradebot/namer"
	"github.com/bvk/tradebot/subcmds/cmdutil"
	"github.com/bvkgo/kv"
	"github.com/visvasity/cli"
)

type Print struct {
	cmdutil.DBFlags
}

func (c *Print) Run(ctx context.Context, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("this command takes one limiter argument")
	}
	arg := args[0]

	getter := func(ctx context.Context, r kv.Reader) error {
		_, uid, _, err := namer.Resolve(ctx, r, arg)
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("could not resolve limiter argument %q: %w", arg, err)
			}
			uid = arg
		}

		key := path.Join(limiter.DefaultKeyspace, uid)
		gv, err := kvutil.Get[gobs.LimiterState](ctx, r, key)
		if err != nil {
			return fmt.Errorf("could not load limiter state from key %q: %w", key, err)
		}

		d, _ := json.MarshalIndent(gv, "", "  ")
		fmt.Printf("%s\n", d)
		return nil
	}

	db, closer, err := c.DBFlags.GetDatabase(ctx)
	if err != nil {
		return err
	}
	defer closer()

	if err := kv.WithReader(ctx, db, getter); err != nil {
		return err
	}
	return nil
}

func (c *Print) Command() (string, *flag.FlagSet, cli.CmdFunc) {
	fset := flag.NewFlagSet("print", flag.ContinueOnError)
	c.DBFlags.SetFlags(fset)
	return "print", fset, cli.CmdFunc(c.Run)
}

func (c *Print) Purpose() string {
	return "Prints a limiter gob in JSON format"
}
