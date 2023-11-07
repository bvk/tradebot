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
)

type Resume struct {
	subcmds.ClientFlags
}

func (c *Resume) Command() (*flag.FlagSet, cli.CmdFunc) {
	fset := flag.NewFlagSet("resume", flag.ContinueOnError)
	c.ClientFlags.SetFlags(fset)
	return fset, cli.CmdFunc(c.run)
}

func (c *Resume) run(ctx context.Context, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("this command takes one (uuid) argument")
	}
	req := &api.ResumeRequest{
		UID: args[0],
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
