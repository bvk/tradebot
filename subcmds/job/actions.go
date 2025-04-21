// Copyright (c) 2023 BVK Chaitanya

package job

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/bvk/tradebot/namer"
	"github.com/bvk/tradebot/server"
	"github.com/bvk/tradebot/subcmds/cmdutil"
	"github.com/bvkgo/kv"
	"github.com/visvasity/cli"
)

type Actions struct {
	cmdutil.DBFlags
}

func (c *Actions) Command() (string, *flag.FlagSet, cli.CmdFunc) {
	fset := new(flag.FlagSet)
	c.DBFlags.SetFlags(fset)
	return "actions", fset, cli.CmdFunc(c.run)
}

func (c *Actions) run(ctx context.Context, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("this command takes one (job-name or uid) argument")
	}
	jobArg := args[0]

	db, closer, err := c.DBFlags.GetDatabase(ctx)
	if err != nil {
		return fmt.Errorf("could not create db instance: %w", err)
	}
	defer closer()

	printer := func(ctx context.Context, r kv.Reader) error {
		_, uid, typename, err := namer.Resolve(ctx, r, jobArg)
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("could not resolve job argument %q: %w", jobArg, err)
			}
			// Assume jobArg is an uid.
			uid = jobArg
		}

		job, err := server.Load(ctx, r, uid, typename)
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

func (c *Actions) Purpose() string {
	return "Prints all trading actions by the job"
}
