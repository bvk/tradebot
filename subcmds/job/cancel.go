// Copyright (c) 2023 BVK Chaitanya

package job

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"

	"github.com/bvkgo/tradebot/api"
	"github.com/bvkgo/tradebot/cli"
	"github.com/bvkgo/tradebot/subcmds"
)

type Cancel struct {
	subcmds.ClientFlags
}

func (c *Cancel) run(ctx context.Context, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("this command takes one (uuid) argument")
	}
	req := &api.CancelRequest{
		UID: args[0],
	}
	resp, err := subcmds.Post[api.CancelResponse](ctx, &c.ClientFlags, "/trader/cancel", req)
	if err != nil {
		return err
	}
	jsdata, _ := json.MarshalIndent(resp, "", "  ")
	fmt.Printf("%s\n", jsdata)
	return nil
}

func (c *Cancel) Command() (*flag.FlagSet, cli.CmdFunc) {
	fset := flag.NewFlagSet("cancel", flag.ContinueOnError)
	c.ClientFlags.SetFlags(fset)
	return fset, cli.CmdFunc(c.run)
}

func (c *Cancel) Synopsis() string {
	return "Cancels a trading job"
}
