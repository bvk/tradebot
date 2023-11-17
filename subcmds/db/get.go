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
	"github.com/bvk/tradebot/gobs"
)

type Get struct {
	Flags

	typename string
}

func (c *Get) Run(ctx context.Context, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("needs one (key) argument")
	}

	db, err := c.Flags.GetDatabase(ctx)
	if err != nil {
		return err
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

	if c.typename == "" {
		data, err := io.ReadAll(v)
		if err != nil {
			return err
		}
		fmt.Printf("%s\n", data)
		return nil
	}

	x, err := c.unmarshal(v)
	if err != nil {
		return err
	}
	d, _ := json.MarshalIndent(x, "", "  ")
	fmt.Printf("%s\n", d)
	return nil
}

func (c *Get) Command() (*flag.FlagSet, cli.CmdFunc) {
	fset := flag.NewFlagSet("get", flag.ContinueOnError)
	c.Flags.SetFlags(fset)
	fset.StringVar(&c.typename, "typename", "", "when non-empty unmarshals to the selected type")
	return fset, cli.CmdFunc(c.Run)
}

func (c *Get) Synopsis() string {
	return "Prints the value of a key in the database"
}

func (c *Get) unmarshal(r io.Reader) (any, error) {
	var v any
	switch c.typename {
	case "TraderJobState":
		v = new(gobs.TraderJobState)
	case "LimiterState":
		v = new(gobs.LimiterState)
	case "LooperState":
		v = new(gobs.LooperState)
	case "WallerState":
		v = new(gobs.WallerState)
	case "KeyValue":
		v = new(gobs.KeyValue)
	case "NameData":
		v = new(gobs.NameData)
	default:
		return nil, fmt.Errorf("unsupported type name %q", c.typename)
	}

	decoder := gob.NewDecoder(r)
	if err := decoder.Decode(v); err != nil {
		return nil, fmt.Errorf("could not unmarshal into %s value: %w", c.typename, err)
	}
	return v, nil
}
