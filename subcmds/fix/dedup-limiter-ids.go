// Copyright (c) 2023 BVK Chaitanya

package fix

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"path"
	"sort"

	"github.com/bvk/tradebot/gobs"
	"github.com/bvk/tradebot/kvutil"
	"github.com/bvk/tradebot/looper"
	"github.com/bvk/tradebot/server"
	"github.com/bvk/tradebot/subcmds/cmdutil"
	"github.com/bvkgo/kv"
	"github.com/visvasity/cli"
)

type DedupLimiterIDs struct {
	cmdutil.DBFlags

	dryRun bool
}

func (c *DedupLimiterIDs) Command() (string, *flag.FlagSet, cli.CmdFunc) {
	fset := new(flag.FlagSet)
	c.DBFlags.SetFlags(fset)
	fset.BoolVar(&c.dryRun, "dry-run", true, "when true only prints the information")
	return "dedup-limiter-ids", fset, cli.CmdFunc(c.Run)
}

func (c *DedupLimiterIDs) Run(ctx context.Context, args []string) error {
	if len(args) != 0 {
		return fmt.Errorf("command takes no arguments")
	}

	db, closer, err := c.DBFlags.GetDatabase(ctx)
	if err != nil {
		return fmt.Errorf("could not create db instance: %w", err)
	}
	defer closer()

	fixer := func(ctx context.Context, rw kv.ReadWriter) error {
		begin := path.Join(looper.DefaultKeyspace, server.MinUUID)
		end := path.Join(looper.DefaultKeyspace, server.MaxUUID)

		it, err := rw.Ascend(ctx, begin, end)
		if err != nil {
			return err
		}
		defer kv.Close(it)

		for k, _, err := it.Fetch(ctx, false); err == nil; k, _, err = it.Fetch(ctx, true) {
			state, err := kvutil.Get[gobs.LooperState](ctx, rw, k)
			if err != nil {
				return fmt.Errorf("could not load looper state at %q: %w", k, err)
			}
			seen := make(map[string]int)
			for _, v := range state.V2.LimiterIDs {
				seen[v] = seen[v] + 1
			}
			if c.dryRun {
				for id, num := range seen {
					if num > 1 {
						log.Printf("looper %s has limiter id %s duplicated %d times", k, id, num)
					}
				}
				continue
			}
			var deduped []string
			for k := range seen {
				deduped = append(deduped, k)
			}
			sort.Strings(deduped)
			state.V2.LimiterIDs = deduped
			if err := kvutil.Set(ctx, rw, k, state); err != nil {
				return fmt.Errorf("could not save looper state at %q: %w", k, err)
			}
		}

		if _, _, err := it.Fetch(ctx, false); err != nil && !errors.Is(err, io.EOF) {
			return fmt.Errorf("could not scan all loopers: %w", err)
		}
		return nil
	}
	if err := kv.WithReadWriter(ctx, db, fixer); err != nil {
		return fmt.Errorf("could not check/fix all loopers: %w", err)
	}
	return nil
}

func (c *DedupLimiterIDs) Purpose() string {
	return "Check and fix duplicate limiter-ids in all Loopers"
}
