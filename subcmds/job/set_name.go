// Copyright (c) 2023 BVK Chaitanya

package job

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/bvk/tradebot/api"
	"github.com/bvk/tradebot/cli"
	"github.com/bvk/tradebot/namer"
	"github.com/bvk/tradebot/subcmds/cmdutil"
	"github.com/bvkgo/kv"
	"github.com/google/uuid"
)

type SetName struct {
	cmdutil.DBFlags
}

func (c *SetName) Command() (*flag.FlagSet, cli.CmdFunc) {
	fset := flag.NewFlagSet("set-name", flag.ContinueOnError)
	c.DBFlags.SetFlags(fset)
	return fset, cli.CmdFunc(c.run)
}

func (c *SetName) run(ctx context.Context, args []string) error {
	if len(args) != 2 {
		return fmt.Errorf("this command takes two (old-name and new-name) arguments")
	}
	oldName, newName := args[0], args[1]
	if len(oldName) == 0 || len(newName) == 0 {
		return fmt.Errorf("name arguments cannot be empty")
	}

	rename := func(ctx context.Context, rw kv.ReadWriter) error {
		if _, err := uuid.Parse(oldName); err == nil {
			// Check that id doesn't resolve to a name already.
			name, _, _, err := namer.Resolve(ctx, rw, oldName)
			if err != nil {
				if errors.Is(err, os.ErrNotExist) {
					// Use set-name api to assign a name cause we don't have job-type.
					req := &api.SetJobNameRequest{
						UID:     oldName,
						JobName: newName,
					}
					if _, err := cmdutil.Post[api.SetJobNameResponse](ctx, &c.ClientFlags, api.SetJobNamePath, req); err != nil {
						return fmt.Errorf("could not set the job name: %w", err)
					}
					return nil
				}
				return fmt.Errorf("could not determine if id already has name or not: %w", err)
			}
			oldName = name
		}

		if err := namer.Rename(ctx, rw, oldName, newName); err != nil {
			return fmt.Errorf("could not rename: %w", err)
		}
		return nil
	}

	db, closer, err := c.DBFlags.GetDatabase(ctx)
	if err != nil {
		return fmt.Errorf("could not open database client: %w", err)
	}
	defer closer()

	if err := kv.WithReadWriter(ctx, db, rename); err != nil {
		return fmt.Errorf("could not rename: %w", err)
	}
	return nil
}

func (c *SetName) Synopsis() string {
	return "Names or renames a trading job"
}
