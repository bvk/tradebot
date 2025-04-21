// Copyright (c) 2023 BVK Chaitanya

package looper

import (
	"context"
	"flag"
	"fmt"
	"strings"

	"github.com/bvk/tradebot/looper"
	"github.com/bvk/tradebot/subcmds/cmdutil"
	"github.com/bvkgo/kv"
	"github.com/visvasity/cli"
)

type Upgrade struct {
	cmdutil.DBFlags
}

func (c *Upgrade) Run(ctx context.Context, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("this command takes one (key) argument")
	}

	upgrader := func(ctx context.Context, rw kv.ReadWriter) error {
		for _, arg := range args {
			uid := strings.TrimPrefix(arg, looper.DefaultKeyspace)
			l, err := looper.Load(ctx, uid, rw)
			if err != nil {
				return fmt.Errorf("could not load looper at key %q: %w", arg, err)
			}
			if err := l.Save(ctx, rw); err != nil {
				return fmt.Errorf("could not save looper at key %q: %w", arg, err)
			}
		}
		return nil
	}

	db, closer, err := c.DBFlags.GetDatabase(ctx)
	if err != nil {
		return err
	}
	defer closer()

	if err := kv.WithReadWriter(ctx, db, upgrader); err != nil {
		return err
	}
	return nil
}

func (c *Upgrade) Command() (string, *flag.FlagSet, cli.CmdFunc) {
	fset := flag.NewFlagSet("upgrade", flag.ContinueOnError)
	c.DBFlags.SetFlags(fset)
	return "upgrade", fset, cli.CmdFunc(c.Run)
}

func (c *Upgrade) Purpose() string {
	return "Upgrades one or more Looper's persistent state"
}
