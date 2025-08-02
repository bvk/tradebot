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

	"github.com/bvk/tradebot/gobs"
	"github.com/bvk/tradebot/subcmds/cmdutil"
	"github.com/bvkgo/kv"
	"github.com/visvasity/cli"
)

type Restore struct {
	cmdutil.DBFlags

	numOpsPerTx int
}

func (c *Restore) Command() (string, *flag.FlagSet, cli.CmdFunc) {
	fset := new(flag.FlagSet)
	c.DBFlags.SetFlags(fset)
	fset.IntVar(&c.numOpsPerTx, "num-ops-per-tx", 100, "max number of ops per restore transaction")
	return "restore", fset, cli.CmdFunc(c.run)
}

func (c *Restore) Purpose() string {
	return "Restores the database from a backup file"
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

	if err := doClean(ctx, db, c.numOpsPerTx); err != nil {
		return fmt.Errorf("could not clear the database: %w", err)
	}
	if err := doRestore(ctx, r, db, c.numOpsPerTx); err != nil {
		return fmt.Errorf("could not run restore from backup: %w", err)
	}

	return nil
}

func doClean(ctx context.Context, db kv.Database, nops int) error {
	done := false
	clean := func(ctx context.Context, rw kv.ReadWriter) error {
		it, err := rw.Scan(ctx)
		if err != nil {
			return fmt.Errorf("could not create scanning iterator: %w", err)
		}
		defer kv.Close(it)

		count := 0
		for k, _, err := it.Fetch(ctx, false); err == nil; k, _, err = it.Fetch(ctx, true) {
			if err := rw.Delete(ctx, k); err != nil {
				return fmt.Errorf("could not delete key %q: %w", k, err)
			}
			if count++; count >= nops {
				break
			}
		}
		if _, _, err := it.Fetch(ctx, false); err != nil && !errors.Is(err, io.EOF) {
			return fmt.Errorf("iterator fetch has failed: %w", err)
		}

		if count < nops {
			done = true
		}
		return nil
	}
	for !done {
		if err := kv.WithReadWriter(ctx, db, clean); err != nil {
			return fmt.Errorf("could not clean database: %w", err)
		}
	}
	return nil
}

// FIXME: Reuse from the cmdutil package.
func doRestore(ctx context.Context, r io.Reader, db kv.Database, nops int) error {
	decoder := gob.NewDecoder(r)
	done := false

	restore := func(ctx context.Context, w kv.ReadWriter) (err error) {
		count := 0
		var item gobs.KeyValue
		for err = decoder.Decode(&item); err == nil; err = decoder.Decode(&item) {
			if err := w.Set(ctx, item.Key, bytes.NewReader(item.Value)); err != nil {
				return fmt.Errorf("could not restore at key %q: %w", item.Key, err)
			}
			item = gobs.KeyValue{}
			if count++; count >= nops {
				break
			}
		}
		if err != nil {
			if !errors.Is(err, io.EOF) {
				return fmt.Errorf("could not decode item from backup file: %w", err)
			}
			done = true
		}
		return nil
	}

	for !done {
		if err := kv.WithReadWriter(ctx, db, restore); err != nil {
			return fmt.Errorf("could not run restore with a transaction: %w", err)
		}
	}
	return nil
}
