// Copyright (c) 2023 BVK Chaitanya

package job

import (
	"context"
	"flag"
	"fmt"
	"path"

	"github.com/bvk/tradebot/cli"
	"github.com/bvk/tradebot/namer"
	"github.com/bvk/tradebot/server"
	"github.com/bvk/tradebot/subcmds/cmdutil"
	"github.com/bvkgo/kv"
	"github.com/google/uuid"
)

type Rename struct {
	cmdutil.DBFlags
}

func (c *Rename) Command() (*flag.FlagSet, cli.CmdFunc) {
	fset := flag.NewFlagSet("rename", flag.ContinueOnError)
	c.DBFlags.SetFlags(fset)
	return fset, cli.CmdFunc(c.run)
}

func (c *Rename) run(ctx context.Context, args []string) error {
	if len(args) != 2 {
		return fmt.Errorf("this command takes two (old-name and new-name) arguments")
	}
	oldName, newName := args[0], args[1]
	if len(oldName) == 0 || len(newName) == 0 {
		return fmt.Errorf("name arguments cannot be empty")
	}

	db, err := c.DBFlags.GetDatabase(ctx)
	if err != nil {
		return fmt.Errorf("could not open database client: %w", err)
	}

	checkAndRename := func(ctx context.Context, rw kv.ReadWriter) error {
		if _, err := uuid.Parse(oldName); err == nil {
			jobKey := path.Join(server.JobsKeyspace, oldName)
			if _, err := rw.Get(ctx, jobKey); err != nil {
				return fmt.Errorf("could not fetch job with id %q: %w", oldName, err)
			}
		}
		if err := namer.Upgrade(ctx, rw, oldName); err != nil {
			return fmt.Errorf("could not upgrade: %w", err)
		}
		return namer.Rename(ctx, rw, oldName, newName)
	}
	if err := kv.WithReadWriter(ctx, db, checkAndRename); err != nil {
		return fmt.Errorf("could not rename: %w", err)
	}
	return nil
}

func (c *Rename) Synopsis() string {
	return "Names or renames a trading job"
}
