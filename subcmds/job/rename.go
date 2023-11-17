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
	"github.com/google/uuid"
)

type Rename struct {
	subcmds.ClientFlags
}

func (c *Rename) Command() (*flag.FlagSet, cli.CmdFunc) {
	fset := flag.NewFlagSet("rename", flag.ContinueOnError)
	c.ClientFlags.SetFlags(fset)
	return fset, cli.CmdFunc(c.run)
}

func (c *Rename) run(ctx context.Context, args []string) error {
	if len(args) != 2 {
		return fmt.Errorf("this command takes two (old-name and new-name) arguments")
	}
	oldName, newName := args[0], args[1]

	req := &api.JobRenameRequest{
		NewName: newName,
	}
	if _, err := uuid.Parse(oldName); err == nil {
		req.UID = oldName
	} else {
		req.OldName = oldName
	}
	resp, err := subcmds.Post[api.JobRenameResponse](ctx, &c.ClientFlags, api.JobRenamePath, req)
	if err != nil {
		return err
	}

	jsdata, _ := json.MarshalIndent(resp, "", "  ")
	fmt.Printf("%s\n", jsdata)
	return nil
}

func (c *Rename) Synopsis() string {
	return "Names or renames a trading job"
}
