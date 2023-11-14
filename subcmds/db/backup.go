// Copyright (c) 2023 BVK Chaitanya

package db

import (
	"bufio"
	"context"
	"encoding/gob"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/bvk/tradebot/cli"
	"github.com/bvk/tradebot/gobs"
	"github.com/bvkgo/kv"
)

type Backup struct {
	Flags
}

func (c *Backup) run(ctx context.Context, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("command takes one (output backup file) argument")
	}

	fp, err := os.OpenFile(args[0], os.O_CREATE|os.O_WRONLY, os.FileMode(0600))
	if err != nil {
		return fmt.Errorf("could not open file %q: %w", args[0], err)
	}
	defer fp.Close()

	bw := bufio.NewWriter(fp)
	// gw := gzip.NewWriter(bw)

	encoder := gob.NewEncoder(bw)
	backup := func(ctx context.Context, r kv.Reader) error {
		it, err := r.Scan(ctx)
		if err != nil {
			return fmt.Errorf("could not create scanning iterator: %w", err)
		}
		defer kv.Close(it)

		for k, v, err := it.Fetch(ctx, false); err == nil; k, v, err = it.Fetch(ctx, true) {
			value, err := io.ReadAll(v)
			if err != nil {
				return fmt.Errorf("could not read value at key %q: %w", k, err)
			}
			item := &gobs.KeyValue{
				Key:   k,
				Value: value,
			}
			if err := encoder.Encode(item); err != nil {
				return fmt.Errorf("could not encode key/value item: %w", err)
			}
		}
		if _, _, err := it.Fetch(ctx, false); err != nil && !errors.Is(err, io.EOF) {
			return fmt.Errorf("iterator fetch has failed: %w", err)
		}
		return nil
	}

	db, err := c.Flags.GetDatabase(ctx)
	if err != nil {
		return fmt.Errorf("could not get database instance: %w", err)
	}
	if err := kv.WithReader(ctx, db, backup); err != nil {
		return fmt.Errorf("could not run backup on the snapshot: %w", err)
	}

	// if gw != nil {
	// 	if err := gw.Flush(); err != nil {
	// 		return fmt.Errorf("could not flush the gzip writer: %w", err)
	// 	}
	// 	if err := gw.Close(); err != nil {
	// 		return fmt.Errorf("could not close gzip stream: %w", err)
	// 	}
	// }

	if err := bw.Flush(); err != nil {
		return fmt.Errorf("could not flush the bufio writer: %w", err)
	}
	if err := fp.Sync(); err != nil {
		return fmt.Errorf("could not sync the output file: %w", err)
	}

	return nil
}

func (c *Backup) Command() (*flag.FlagSet, cli.CmdFunc) {
	fset := flag.NewFlagSet("backup", flag.ContinueOnError)
	c.Flags.SetFlags(fset)
	return fset, cli.CmdFunc(c.run)
}

func (c *Backup) Synopsis() string {
	return "Takes a backup of the database into a file"
}
