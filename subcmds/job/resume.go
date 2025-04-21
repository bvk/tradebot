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
	"github.com/bvk/tradebot/namer"
	"github.com/bvk/tradebot/subcmds/cmdutil"
	"github.com/visvasity/cli"
)

type Resume struct {
	cmdutil.DBFlags
}

func (c *Resume) Command() (string, *flag.FlagSet, cli.CmdFunc) {
	fset := new(flag.FlagSet)
	c.DBFlags.SetFlags(fset)
	return "resume", fset, cli.CmdFunc(c.run)
}

func (c *Resume) run(ctx context.Context, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("this command takes one (job-id) argument")
	}
	jobArg := args[0]

	db, closer, err := c.DBFlags.GetDatabase(ctx)
	if err != nil {
		return fmt.Errorf("could not create database client: %w", err)
	}
	defer closer()

	_, uid, _, err := namer.ResolveDB(ctx, db, jobArg)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("could not resolve job argument %q: %w", jobArg, err)
		}
		uid = jobArg
	}

	req := &api.JobResumeRequest{
		UID: uid,
	}
	resp, err := cmdutil.Post[api.JobResumeResponse](ctx, &c.ClientFlags, api.JobResumePath, req)
	if err != nil {
		return err
	}
	jsdata, _ := json.MarshalIndent(resp, "", "  ")
	fmt.Printf("%s\n", jsdata)
	return nil
}

func (c *Resume) Purpose() string {
	return "Resumes a trading job"
}
