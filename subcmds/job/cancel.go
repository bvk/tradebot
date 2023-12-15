// Copyright (c) 2023 BVK Chaitanya

package job

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/bvk/tradebot/api"
	"github.com/bvk/tradebot/cli"
	"github.com/bvk/tradebot/namer"
	"github.com/bvk/tradebot/subcmds/cmdutil"
)

type Cancel struct {
	cmdutil.DBFlags
}

func (c *Cancel) run(ctx context.Context, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("this command takes one (job-id) argument")
	}
	jobArg := args[0]

	db, closer, err := c.DBFlags.GetDatabase(ctx)
	if err != nil {
		return fmt.Errorf("could not create database client: %w", err)
	}
	defer closer()

	uid, _, err := namer.Resolve(ctx, db, jobArg)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("could not resolve job argument %q: %w", jobArg, err)
		}
		uid = jobArg
	}

	req := &api.JobCancelRequest{
		UID: uid,
	}
	resp, err := cmdutil.Post[api.JobCancelResponse](ctx, &c.ClientFlags, api.JobCancelPath, req)
	if err != nil {
		return err
	}
	jsdata, _ := json.MarshalIndent(resp, "", "  ")
	fmt.Printf("%s\n", jsdata)
	return nil
}

func (c *Cancel) Command() (*flag.FlagSet, cli.CmdFunc) {
	fset := flag.NewFlagSet("cancel", flag.ContinueOnError)
	c.DBFlags.SetFlags(fset)
	return fset, cli.CmdFunc(c.run)
}

func (c *Cancel) Synopsis() string {
	return "Cancels a trading job"
}
