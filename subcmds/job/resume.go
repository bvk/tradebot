// Copyright (c) 2023 BVK Chaitanya

package job

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"strings"

	"github.com/bvk/tradebot/api"
	"github.com/bvk/tradebot/cli"
	"github.com/bvk/tradebot/subcmds"
	"github.com/bvk/tradebot/subcmds/db"
	"github.com/bvk/tradebot/trader"
	"github.com/google/uuid"
)

type Resume struct {
	db.Flags
}

func (c *Resume) Command() (*flag.FlagSet, cli.CmdFunc) {
	fset := flag.NewFlagSet("resume", flag.ContinueOnError)
	c.Flags.SetFlags(fset)
	return fset, cli.CmdFunc(c.run)
}

func (c *Resume) run(ctx context.Context, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("this command takes one (job-id) argument")
	}

	jobID := args[0]
	if strings.HasPrefix(jobID, "name:") {
		v, err := c.Flags.ResolveName(ctx, jobID)
		if err != nil {
			return fmt.Errorf("could not resolve job name %q: %w", jobID, err)
		}
		jobID = v
	}

	if strings.HasPrefix(jobID, "uuid:") {
		jobID = strings.TrimPrefix(jobID, "uuid:")
	} else if strings.HasPrefix(jobID, trader.JobsKeyspace) {
		jobID = strings.TrimPrefix(jobID, trader.JobsKeyspace)
	}

	if _, err := uuid.Parse(jobID); err != nil {
		return fmt.Errorf("could not parse job id value %q as an uuid: %w", jobID, err)
	}

	req := &api.ResumeRequest{
		UID: jobID,
	}
	resp, err := subcmds.Post[api.ResumeResponse](ctx, &c.ClientFlags, "/trader/resume", req)
	if err != nil {
		return err
	}
	jsdata, _ := json.MarshalIndent(resp, "", "  ")
	fmt.Printf("%s\n", jsdata)
	return nil
}

func (c *Resume) Synopsis() string {
	return "Resumes a trading job"
}
