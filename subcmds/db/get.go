// Copyright (c) 2023 BVK Chaitanya

package db

import (
	"context"
	"encoding/gob"
	"encoding/json"
	"flag"
	"fmt"
	"io"

	"github.com/bvk/tradebot/gobs"
	"github.com/bvk/tradebot/subcmds/cmdutil"
	"github.com/bvkgo/kv/kvhttp"
	"github.com/visvasity/cli"
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

	// Without a local value-type, ask a remote server to convert the value to
	// JSON (the server picks the concrete type from the key). Non-http databases
	// don't support this and fall through to printing the raw bytes.
	if c.valueType == "" {
		if hdb, ok := db.(*kvhttp.DB); ok {
			hdb.SetAccept("application/json")
		}
	}

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

	value, err := gobs.NewByTypename(c.valueType)
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

func (c *Get) Command() (string, *flag.FlagSet, cli.CmdFunc) {
	fset := new(flag.FlagSet)
	c.DBFlags.SetFlags(fset)
	fset.StringVar(&c.valueType, "value-type", "", "when non-empty unmarshals to the selected type")
	return "get", fset, cli.CmdFunc(c.Run)
}

func (c *Get) Purpose() string {
	return "Prints the value of a key in the database"
}
