// Copyright (c) 2023 BVK Chaitanya

package job

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"

	"github.com/bvk/tradebot/api"
	"github.com/bvk/tradebot/cli"
	"github.com/bvk/tradebot/subcmds/cmdutil"
)

type List struct {
	cmdutil.ClientFlags
}

func (c *List) Command() (*flag.FlagSet, cli.CmdFunc) {
	fset := flag.NewFlagSet("list", flag.ContinueOnError)
	c.ClientFlags.SetFlags(fset)
	return fset, cli.CmdFunc(c.run)
}

func (c *List) run(ctx context.Context, args []string) error {
	if len(args) != 0 {
		return fmt.Errorf("this command takes no arguments")
	}

	req := &api.JobListRequest{}
	resp, err := cmdutil.Post[api.JobListResponse](ctx, &c.ClientFlags, api.JobListPath, req)
	if err != nil {
		return err
	}
	jsdata, _ := json.MarshalIndent(resp, "", "  ")
	fmt.Printf("%s\n", jsdata)
	return nil
}

func (c *List) Synopsis() string {
	return "Prints trading job ids"
}
