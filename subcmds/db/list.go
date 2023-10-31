// Copyright (c) 2023 BVK Chaitanya

package db

import (
	"context"
	"flag"
	"fmt"

	"github.com/bvkgo/kv"
	"github.com/bvkgo/tradebot/cli"
)

type List struct {
	Flags

	printValues bool
}

func (c *List) Run(ctx context.Context, args []string) error {
	if len(args) != 0 {
		return fmt.Errorf("command takes no arguments")
	}

	// TODO: handle printValues flag

	list := func(ctx context.Context, r kv.Reader) error {
		it, err := r.Scan(ctx)
		if err != nil {
			return err
		}
		defer kv.Close(it)

		for k, _, ok := it.Current(ctx); ok; k, _, ok = it.Next(ctx) {
			fmt.Println(k)
		}

		if err := it.Err(); err != nil {
			return err
		}
		return nil
	}

	db := c.Flags.Client()
	if err := kv.WithReader(ctx, db, list); err != nil {
		return err
	}
	return nil
}

func (c *List) Command() (*flag.FlagSet, cli.CmdFunc) {
	fset := flag.NewFlagSet("list", flag.ContinueOnError)
	c.Flags.SetFlags(fset)
	fset.BoolVar(&c.printValues, "print-values", false, "values are printed when true")
	return fset, cli.CmdFunc(c.Run)
}

func (c *List) Synopsis() string {
	return "Prints keys and values in the database"
}
