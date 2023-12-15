// Copyright (c) 2023 BVK Chaitanya

package db

import (
	"context"
	"encoding/gob"
	"encoding/json"
	"flag"
	"fmt"
	"io"

	"github.com/bvk/tradebot/cli"
	"github.com/bvk/tradebot/subcmds/cmdutil"
)

type Get struct {
	cmdutil.DBFlags

	valueType string
}

func (c *Get) Run(ctx context.Context, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("needs one (key) argument")
	}

	db, closer, err := c.DBFlags.GetDatabase(ctx)
	if err != nil {
		return err
	}
	defer closer()

	snap, err := db.NewSnapshot(ctx)
	if err != nil {
		return err
	}
	defer snap.Discard(ctx)

	v, err := snap.Get(ctx, args[0])
	if err != nil {
		return err
	}

	if c.valueType == "" {
		data, err := io.ReadAll(v)
		if err != nil {
			return err
		}
		fmt.Printf("%s\n", data)
		return nil
	}

	value, err := TypeNameValue(c.valueType)
	if err != nil {
		return fmt.Errorf("invalid value-type %q: %w", c.valueType, err)
	}

	if err := gob.NewDecoder(v).Decode(value); err != nil {
		return fmt.Errorf("could not gob-decode into %s value: %w", c.valueType, err)
	}

	d, _ := json.MarshalIndent(value, "", "  ")
	fmt.Printf("%s\n", d)
	return nil
}

func (c *Get) Command() (*flag.FlagSet, cli.CmdFunc) {
	fset := flag.NewFlagSet("get", flag.ContinueOnError)
	c.DBFlags.SetFlags(fset)
	fset.StringVar(&c.valueType, "value-type", "", "when non-empty unmarshals to the selected type")
	return fset, cli.CmdFunc(c.Run)
}

func (c *Get) Synopsis() string {
	return "Prints the value of a key in the database"
}
