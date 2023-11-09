// Copyright (c) 2023 BVK Chaitanya

package waller

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"path"
	"strings"

	"github.com/bvk/tradebot/cli"
	"github.com/bvk/tradebot/kvutil"
	"github.com/bvk/tradebot/subcmds/db"
	"github.com/bvk/tradebot/waller"
	"github.com/bvkgo/kv"
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

		for k, _, err := it.Fetch(ctx, false); err == nil; k, _, err = it.Fetch(ctx, true) {
			if !strings.HasPrefix(k, keyspace) {
				break
			}
			if strings.Contains(k, "/loop-") {
				continue
			}

			gv, err := kvutil.Get[waller.State](ctx, r, k)
			if err != nil {
				return err
			}

			d, _ := json.Marshal(gv)
			fmt.Printf("key=%s value=%s\n", k, d)
		}

		if _, _, err := it.Fetch(ctx, false); err != nil && !errors.Is(err, io.EOF) {
			return err
		}
		return nil
	}

	db, err := c.Flags.GetDatabase()
	if err != nil {
		return err
	}
	if err := kv.WithReader(ctx, db, lister); err != nil {
		return err
	}
	return nil
}

func (c *List) Command() (*flag.FlagSet, cli.CmdFunc) {
	fset := flag.NewFlagSet("list", flag.ContinueOnError)
	c.Flags.SetFlags(fset)
	fset.StringVar(&c.keyspace, "keyspace", waller.DefaultKeyspace, "keyspace prefix in the db")
	return fset, cli.CmdFunc(c.Run)
}

func (c *List) Synopsis() string {
	return "Lists waller jobs under a keyspace"
}
