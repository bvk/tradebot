// Copyright (c) 2023 BVK Chaitanya

package job

import (
	"bytes"
	"context"
	"encoding/gob"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"path"

	"github.com/bvk/tradebot/api"
	"github.com/bvk/tradebot/cli"
	"github.com/bvk/tradebot/gobs"
	"github.com/bvk/tradebot/kvutil"
	"github.com/bvk/tradebot/server"
	"github.com/bvk/tradebot/subcmds/cmdutil"
	"github.com/bvkgo/kv"
)

type Import struct {
	cmdutil.DBFlags
}

func (c *Import) Command() (*flag.FlagSet, cli.CmdFunc) {
	fset := flag.NewFlagSet("import", flag.ContinueOnError)
	c.DBFlags.SetFlags(fset)
	return fset, cli.CmdFunc(c.run)
}

func (c *Import) run(ctx context.Context, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("this command takes one file argument")
	}
	filename := args[0]

	fp, err := os.Open(filename)
	if err != nil {
		return fmt.Errorf("could not open file %q: %w", filename, err)
	}
	defer fp.Close()

	export := new(gobs.JobExportData)
	if err := gob.NewDecoder(fp).Decode(export); err != nil {
		return fmt.Errorf("could not gob-decode data file: %w", err)
	}

	db, err := c.DBFlags.GetDatabase(ctx)
	if err != nil {
		return fmt.Errorf("could not get db access: %w", err)
	}

	// Verify that job name is unused in the target db.
	if len(export.State.JobName) > 0 {
		if _, err := server.ResolveName(ctx, db, export.State.JobName); err == nil {
			return fmt.Errorf("target already has job named %q: %w", export.State.JobName, os.ErrExist)
		}
	}

	// Verify that job key and all other keys doesn't exist in the target db.
	verifier := func(ctx context.Context, r kv.Reader) error {
		jobKey := path.Join(server.JobsKeyspace, export.ID)
		if _, err := r.Get(ctx, jobKey); err == nil {
			return fmt.Errorf("job key %q already exists in the target: %w", jobKey, os.ErrExist)
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("could not check for job key %q: %w", jobKey, err)
		}

		for _, pair := range export.KeyValues {
			if _, err := r.Get(ctx, pair.Key); err == nil {
				return fmt.Errorf("data key %q already exists in the target: %w", pair.Key, err)
			} else if !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("could not check for data key %q: %w", pair.Key, err)
			}
		}
		return nil
	}
	if err := kv.WithReader(ctx, db, verifier); err != nil {
		return fmt.Errorf("cannot import the job to the target db: %w", err)
	}

	jobName := export.State.JobName
	importer := func(ctx context.Context, rw kv.ReadWriter) error {
		// Erase job name cause we will use REST request below.
		export.State.JobName = ""
		export.State.NeedsManualResume = true
		jobKey := path.Join(server.JobsKeyspace, export.ID)
		if err := kvutil.Set(ctx, rw, jobKey, export.State); err != nil {
			return fmt.Errorf("could not import the job state: %w", err)
		}
		for _, pair := range export.KeyValues {
			if err := rw.Set(ctx, pair.Key, bytes.NewReader(pair.Value)); err != nil {
				return fmt.Errorf("could not import key-value pair at key %q: %w", pair.Key, err)
			}
		}
		return nil
	}
	if err := kv.WithReadWriter(ctx, db, importer); err != nil {
		return fmt.Errorf("could not import the job: %w", err)
	}

	req := &api.JobRenameRequest{
		NewName: jobName,
		UID:     export.ID,
	}
	if _, err := cmdutil.Post[api.JobRenameResponse](ctx, &c.DBFlags.ClientFlags, api.JobRenamePath, req); err != nil {
		log.Printf("job with id %s is imported, but could not set the job name (ignored): %v", export.ID, err)
	}

	// TODO: Verify that job can be loaded successfully.

	return nil
}

func (c *Import) Synopsis() string {
	return "Imports a trading job from a file"
}
