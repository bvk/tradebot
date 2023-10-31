// Copyright (c) 2023 BVK Chaitanya

package looper

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"path"
	"strings"

	"github.com/bvkgo/kv"
	"github.com/bvkgo/tradebot/cli"
	"github.com/bvkgo/tradebot/kvutil"
	"github.com/bvkgo/tradebot/looper"
	"github.com/bvkgo/tradebot/subcmds/db"
)

type List struct {
	db.Flags

	keyspace string
}

func (c *List) Run(ctx context.Context, args []string) error {
	if len(args) != 0 {
		return fmt.Errorf("this command takes no arguments")
	}

	keyspace := path.Clean(c.keyspace + "/")
	lister := func(ctx context.Context, r kv.Reader) error {
		it, err := r.Ascend(ctx, keyspace, "")
		if err != nil {
			return err
		}
		defer kv.Close(it)

		for k, _, ok := it.Current(ctx); ok; k, _, ok = it.Next(ctx) {
			if !strings.HasPrefix(k, keyspace) {
				break
			}

			gv, err := kvutil.Get[looper.State](ctx, r, k)
			if err != nil {
				return err
			}

			d, _ := json.Marshal(gv)
			fmt.Printf("%s\n", d)
		}

		if err := it.Err(); err != nil {
			return err
		}
		return nil
	}

	db := c.Flags.Client()
	if err := kv.WithReader(ctx, db, lister); err != nil {
		return err
	}
	return nil
}

func (c *List) Command() (*flag.FlagSet, cli.CmdFunc) {
	fset := flag.NewFlagSet("list", flag.ContinueOnError)
	c.Flags.SetFlags(fset)
	fset.StringVar(&c.keyspace, "keyspace", looper.DefaultKeyspace, "keyspace prefix in the db")
	return fset, cli.CmdFunc(c.Run)
}

func (c *List) Synopsis() string {
	return "Lists buy-sell loop jobs under a keyspace"
}
