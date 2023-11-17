// Copyright (c) 2023 BVK Chaitanya

package job

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"

	"github.com/bvk/tradebot/api"
	"github.com/bvk/tradebot/cli"
	"github.com/bvk/tradebot/subcmds"
	"github.com/bvk/tradebot/subcmds/db"
)

type Pause struct {
	db.Flags
}

func (c *Pause) Command() (*flag.FlagSet, cli.CmdFunc) {
	fset := flag.NewFlagSet("pause", flag.ContinueOnError)
	c.Flags.SetFlags(fset)
	return fset, cli.CmdFunc(c.run)
}

func (c *Pause) run(ctx context.Context, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("this command takes one (job-id) argument")
	}

	jobID, err := c.Flags.GetJobID(ctx, args[0])
	if err != nil {
		return fmt.Errorf("could not convert argument %q to job id: %w", jobID, err)
	}

	req := &api.JobPauseRequest{
		UID: jobID,
	}
	resp, err := subcmds.Post[api.JobPauseResponse](ctx, &c.ClientFlags, api.JobPausePath, req)
	if err != nil {
		return err
	}
	jsdata, _ := json.MarshalIndent(resp, "", "  ")
	fmt.Printf("%s\n", jsdata)
	return nil
}

func (c *Pause) Synopsis() string {
	return "Pauses a trading job"
}
