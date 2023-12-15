// Copyright (c) 2023 BVK Chaitanya

package db

import (
	"bufio"
	"bytes"
	"context"
	"encoding/gob"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/bvk/tradebot/cli"
	"github.com/bvk/tradebot/gobs"
	"github.com/bvk/tradebot/subcmds/cmdutil"
	"github.com/bvkgo/kv"
)

type Restore struct {
	cmdutil.DBFlags
}

func (c *Restore) run(ctx context.Context, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("command takes one (input backup file) argument")
	}

	fp, err := os.Open(args[0])
	if err != nil {
		return fmt.Errorf("could not open file %q: %w", args[0], err)
	}
	defer fp.Close()

	r := bufio.NewReader(fp)

	db, closer, err := c.DBFlags.GetDatabase(ctx)
	if err != nil {
		return fmt.Errorf("could not get database instance: %w", err)
	}
	defer closer()

	if err := doRestore(ctx, r, db); err != nil {
		return fmt.Errorf("could not run restore from backup: %w", err)
	}

	return nil
}

func (c *Restore) Command() (*flag.FlagSet, cli.CmdFunc) {
	fset := flag.NewFlagSet("restore", flag.ContinueOnError)
	c.DBFlags.SetFlags(fset)
	return fset, cli.CmdFunc(c.run)
}

func (c *Restore) Synopsis() string {
	return "Restores the database from a backup file"
}

// FIXME: Reuse from the cmdutil package.
func doRestore(ctx context.Context, r io.Reader, db kv.Database) error {
	decoder := gob.NewDecoder(r)
	restore := func(ctx context.Context, w kv.ReadWriter) error {
		it, err := w.Scan(ctx)
		if err != nil {
			return fmt.Errorf("could not create scanning iterator: %w", err)
		}
		defer kv.Close(it)
		for k, _, err := it.Fetch(ctx, false); err == nil; k, _, err = it.Fetch(ctx, true) {
			if err := w.Delete(ctx, k); err != nil {
				return fmt.Errorf("could not delete key %q: %w", k, err)
			}
		}
		if _, _, err := it.Fetch(ctx, false); err != nil && !errors.Is(err, io.EOF) {
			return fmt.Errorf("iterator fetch has failed: %w", err)
		}

		var item gobs.KeyValue
		for err = decoder.Decode(&item); err == nil; err = decoder.Decode(&item) {
			if err := w.Set(ctx, item.Key, bytes.NewReader(item.Value)); err != nil {
				return fmt.Errorf("could not restore at key %q: %w", item.Key, err)
			}
			item = gobs.KeyValue{}
		}

		if !errors.Is(err, io.EOF) {
			return fmt.Errorf("could not decode item from backup file: %w", err)
		}
		return nil
	}

	if err := kv.WithReadWriter(ctx, db, restore); err != nil {
		return fmt.Errorf("could not run restore with a transaction: %w", err)
	}
	return nil
}
