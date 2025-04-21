// Copyright (c) 2023 BVK Chaitanya

package db

import (
	"context"
	"flag"
	"fmt"
	"strings"

	"github.com/bvk/tradebot/subcmds/cmdutil"
	"github.com/visvasity/cli"
)

type Set struct {
	cmdutil.DBFlags
}

func (c *Set) Run(ctx context.Context, args []string) error {
	if len(args) != 2 {
		return fmt.Errorf("needs two (key, value) arguments")
	}

	db, closer, err := c.DBFlags.GetDatabase(ctx)
	if err != nil {
		return err
	}
	defer closer()

	tx, err := db.NewTransaction(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if err := tx.Set(ctx, args[0], strings.NewReader(args[1])); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	return nil
}

func (c *Set) Command() (string, *flag.FlagSet, cli.CmdFunc) {
	fset := new(flag.FlagSet)
	c.DBFlags.SetFlags(fset)
	return "set", fset, cli.CmdFunc(c.Run)
}

func (c *Set) Purpose() string {
	return "Updates the value for a key in the database"
}
