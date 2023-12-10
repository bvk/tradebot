// Copyright (c) 2023 BVK Chaitanya

package job

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/bvk/tradebot/cli"
	"github.com/bvk/tradebot/namer"
	"github.com/bvk/tradebot/server"
	"github.com/bvk/tradebot/subcmds/cmdutil"
	"github.com/bvkgo/kv"
)

type Actions struct {
	cmdutil.DBFlags
}

func (c *Actions) Command() (*flag.FlagSet, cli.CmdFunc) {
	fset := flag.NewFlagSet("actions", flag.ContinueOnError)
	c.DBFlags.SetFlags(fset)
	return fset, cli.CmdFunc(c.run)
}

func (c *Actions) run(ctx context.Context, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("this command takes one (job-name or uid) argument")
	}
	jobArg := args[0]

	db, err := c.DBFlags.GetDatabase(ctx)
	if err != nil {
		return fmt.Errorf("could not create db instance: %w", err)
	}

	printer := func(ctx context.Context, r kv.Reader) error {
		uid, _, err := namer.ResolveName(ctx, r, jobArg)
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("could not resolve job argument %q: %w", jobArg, err)
			}
			// Assume jobArg is an uid.
			uid = jobArg
		}

		job, err := server.Load(ctx, r, uid)
		if err != nil {
			return fmt.Errorf("could not load job with uid %q: %w", uid, err)
		}
		actions := job.Actions()
		js, _ := json.MarshalIndent(actions, "", "  ")
		fmt.Printf("%s\n", js)
		return nil
	}
	if err := kv.WithReader(ctx, db, printer); err != nil {
		return fmt.Errorf("could not print job actions: %w", err)
	}

	return nil
}

func (c *Actions) Synopsis() string {
	return "Prints all trading actions by the job"
}
