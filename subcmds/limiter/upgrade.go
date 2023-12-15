// Copyright (c) 2023 BVK Chaitanya

package limiter

import (
	"context"
	"flag"
	"fmt"
	"strings"

	"github.com/bvk/tradebot/cli"
	"github.com/bvk/tradebot/limiter"
	"github.com/bvk/tradebot/subcmds/cmdutil"
	"github.com/bvkgo/kv"
)

type Upgrade struct {
	cmdutil.DBFlags
}

func (c *Upgrade) Run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("this command takes one or more (limiter db keys) arguments")
	}

	upgrader := func(ctx context.Context, rw kv.ReadWriter) error {
		for _, arg := range args {
			uid := strings.TrimPrefix(arg, limiter.DefaultKeyspace)
			l, err := limiter.Load(ctx, uid, rw)
			if err != nil {
				return fmt.Errorf("could not load limiter at key %q: %w", arg, err)
			}
			if err := l.Save(ctx, rw); err != nil {
				return fmt.Errorf("could not save limiter at key %q: %w", arg, err)
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

func (c *Upgrade) Command() (*flag.FlagSet, cli.CmdFunc) {
	fset := flag.NewFlagSet("upgrade", flag.ContinueOnError)
	c.DBFlags.SetFlags(fset)
	return fset, cli.CmdFunc(c.Run)
}

func (c *Upgrade) Synopsis() string {
	return "Upgrades one or more Limiters' persistent state"
}
