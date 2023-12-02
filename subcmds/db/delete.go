// Copyright (c) 2023 BVK Chaitanya

package db

import (
	"context"
	"flag"
	"fmt"

	"github.com/bvk/tradebot/cli"
	"github.com/bvk/tradebot/subcmds/cmdutil"
)

type Delete struct {
	cmdutil.DBFlags
}

func (c *Delete) Run(ctx context.Context, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("needs one (key) argument")
	}

	db, err := c.DBFlags.GetDatabase(ctx)
	if err != nil {
		return err
	}
	tx, err := db.NewTransaction(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if err := tx.Delete(ctx, args[0]); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	return nil
}

func (c *Delete) Command() (*flag.FlagSet, cli.CmdFunc) {
	fset := flag.NewFlagSet("delete", flag.ContinueOnError)
	c.DBFlags.SetFlags(fset)
	return fset, cli.CmdFunc(c.Run)
}

func (c *Delete) Synopsis() string {
	return "Deletes a key in the database"
}
