// Copyright (c) 2023 BVK Chaitanya

package job

import (
	"context"
	"flag"
	"fmt"
	"os"
	"text/tabwriter"

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

	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 1, ' ', tabwriter.AlignRight)
	fmt.Fprintf(tw, "Name\tUID\tType\tStatus\t\n")
	for _, job := range resp.Jobs {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t\n", job.Name, job.UID, job.Type, job.State)
	}
	tw.Flush()
	return nil
}

func (c *List) Synopsis() string {
	return "Prints trading job ids"
}
