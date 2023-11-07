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

type Pause struct {
	subcmds.ClientFlags
}

func (c *Pause) Command() (*flag.FlagSet, cli.CmdFunc) {
	fset := flag.NewFlagSet("pause", flag.ContinueOnError)
	c.ClientFlags.SetFlags(fset)
	return fset, cli.CmdFunc(c.run)
}

func (c *Pause) run(ctx context.Context, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("this command takes one (uuid) argument")
	}
	req := &api.PauseRequest{
		UID: args[0],
	}
	resp, err := subcmds.Post[api.PauseResponse](ctx, &c.ClientFlags, "/trader/pause", req)
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
