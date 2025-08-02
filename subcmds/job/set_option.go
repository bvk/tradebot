// Copyright (c) 2023 BVK Chaitanya

package job

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/bvk/tradebot/api"
	"github.com/bvk/tradebot/namer"
	"github.com/bvk/tradebot/subcmds/cmdutil"
	"github.com/visvasity/cli"
)

type SetOption struct {
	cmdutil.DBFlags
}

func (c *SetOption) Command() (string, *flag.FlagSet, cli.CmdFunc) {
	fset := flag.NewFlagSet("set-option", flag.ContinueOnError)
	c.DBFlags.SetFlags(fset)
	return "set-option", fset, cli.CmdFunc(c.run)
}

func (c *SetOption) Purpose() string {
	return "Update a trading job options"
}

func (c *SetOption) run(ctx context.Context, args []string) error {
	if len(args) != 2 {
		return fmt.Errorf("this command takes two (job-id, opt=value) arguments")
	}
	jobArg := args[0]
	var optKey, optVal string
	if p := strings.IndexRune(args[1], '='); p != -1 {
		optKey, optVal = args[1][:p], args[1][p+1:]
	}
	if optKey == "" || optVal == "" {
		return fmt.Errorf("option argument must be in key=value form")
	}

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

	req := &api.JobSetOptionRequest{
		UID:         uid,
		OptionKey:   optKey,
		OptionValue: optVal,
	}
	resp, err := cmdutil.Post[api.JobSetOptionResponse](ctx, &c.ClientFlags, api.JobSetOptionPath, req)
	if err != nil {
		return err
	}
	jsdata, _ := json.MarshalIndent(resp, "", "  ")
	fmt.Printf("%s\n", jsdata)
	return nil
}
