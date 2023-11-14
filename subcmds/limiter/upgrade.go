// Copyright (c) 2023 BVK Chaitanya

package limiter

import (
	"context"
	"flag"
	"fmt"

	"github.com/bvk/tradebot/cli"
	"github.com/bvk/tradebot/limiter"
	"github.com/bvk/tradebot/subcmds/db"
	"github.com/bvkgo/kv"
)

type Upgrade struct {
	db.Flags
}

func (c *Upgrade) Run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("this command takes one or more (limiter db keys) arguments")
	}

	upgrader := func(ctx context.Context, rw kv.ReadWriter) error {
		for _, arg := range args {
			l, err := limiter.Load(ctx, arg, rw)
			if err != nil {
				return fmt.Errorf("could not load limiter at key %q: %w", arg, err)
			}
			if err := l.Save(ctx, rw); err != nil {
				return fmt.Errorf("could not save limiter at key %q: %w", arg, err)
			}
		}
		return nil
	}

	db, err := c.Flags.GetDatabase(ctx)
	if err != nil {
		return err
	}
	if err := kv.WithReadWriter(ctx, db, upgrader); err != nil {
		return err
	}
	return nil
}

func (c *Upgrade) Command() (*flag.FlagSet, cli.CmdFunc) {
	fset := flag.NewFlagSet("upgrade", flag.ContinueOnError)
	c.Flags.SetFlags(fset)
	return fset, cli.CmdFunc(c.Run)
}

func (c *Upgrade) Synopsis() string {
	return "Upgrades one or more Limiters' persistent state"
}
