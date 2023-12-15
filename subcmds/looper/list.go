// Copyright (c) 2023 BVK Chaitanya

package looper

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"path"
	"strings"

	"github.com/bvk/tradebot/cli"
	"github.com/bvk/tradebot/looper"
	"github.com/bvk/tradebot/subcmds/cmdutil"
	"github.com/bvkgo/kv"
)

type List struct {
	cmdutil.DBFlags

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

		for k, _, err := it.Fetch(ctx, false); err == nil; k, _, err = it.Fetch(ctx, true) {
			if !strings.HasPrefix(k, keyspace) {
				break
			}
			if strings.Contains(k, "/buy-") || strings.Contains(k, "/sell-") {
				continue
			}

			fmt.Println(k)
		}

		if _, _, err := it.Fetch(ctx, false); err != nil && !errors.Is(err, io.EOF) {
			return err
		}
		return nil
	}

	db, closer, err := c.DBFlags.GetDatabase(ctx)
	if err != nil {
		return err
	}
	defer closer()

	if err := kv.WithReader(ctx, db, lister); err != nil {
		return err
	}
	return nil
}

func (c *List) Command() (*flag.FlagSet, cli.CmdFunc) {
	fset := flag.NewFlagSet("list", flag.ContinueOnError)
	c.DBFlags.SetFlags(fset)
	fset.StringVar(&c.keyspace, "keyspace", looper.DefaultKeyspace, "keyspace prefix in the db")
	return fset, cli.CmdFunc(c.Run)
}

func (c *List) Synopsis() string {
	return "Lists buy-sell loop jobs under a keyspace"
}
