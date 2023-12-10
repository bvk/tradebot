// Copyright (c) 2023 BVK Chaitanya

package job

import (
	"bufio"
	"context"
	"encoding/gob"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path"

	"github.com/bvk/tradebot/cli"
	"github.com/bvk/tradebot/gobs"
	"github.com/bvk/tradebot/job"
	"github.com/bvk/tradebot/kvutil"
	"github.com/bvk/tradebot/namer"
	"github.com/bvk/tradebot/server"
	"github.com/bvk/tradebot/subcmds/cmdutil"
	"github.com/bvk/tradebot/trader"
	"github.com/bvkgo/kv"
	"github.com/bvkgo/kv/kvmemdb"
)

type Export struct {
	cmdutil.DBFlags

	outfile string
}

func (c *Export) Command() (*flag.FlagSet, cli.CmdFunc) {
	fset := flag.NewFlagSet("export", flag.ContinueOnError)
	c.DBFlags.SetFlags(fset)
	fset.StringVar(&c.outfile, "output", "", "Output file name for the exported data.")
	return fset, cli.CmdFunc(c.run)
}

func (c *Export) run(ctx context.Context, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("this command takes one (job-id) argument")
	}
	if len(c.outfile) == 0 {
		return fmt.Errorf("output file name must be specified")
	}

	db, err := c.DBFlags.GetDatabase(ctx)
	if err != nil {
		return fmt.Errorf("could not get db access: %w", err)
	}

	jobID, err := c.DBFlags.GetJobID(ctx, args[0])
	if err != nil {
		return fmt.Errorf("could not convert argument %q to job id: %w", jobID, err)
	}

	jobKey := path.Join(server.JobsKeyspace, jobID)
	jstate, err := kvutil.GetDB[gobs.ServerJobState](ctx, db, jobKey)
	if err != nil {
		return fmt.Errorf("could not load job state: %w", err)
	}
	if !job.IsStopped(job.State(jstate.CurrentState)) {
		return fmt.Errorf("job is actively running; cannot be exported")
	}

	var job trader.Job
	var name, typename string
	loader := func(ctx context.Context, r kv.Reader) (err error) {
		job, err = server.Load(ctx, r, jobID)
		if err != nil {
			return fmt.Errorf("could not load job: %w", err)
		}

		name, typename, err = namer.ResolveID(ctx, r, jobID)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("could not resolve job id to name: %w", err)
		}
		return
	}
	if err := kv.WithReader(ctx, db, loader); err != nil {
		return fmt.Errorf("could not load trader job: %w", err)
	}

	memdb := kvmemdb.New()
	if err := kv.WithReadWriter(ctx, memdb, job.Save); err != nil {
		return fmt.Errorf("could not save job to a temporary memdb: %w", err)
	}

	export := &gobs.JobExportData{
		ID:       jobID,
		Name:     name,
		Typename: typename,
		State:    jstate,
	}
	iterate := func(ctx context.Context, r kv.Reader) error {
		it, err := r.Scan(ctx)
		if err != nil {
			return err
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
			export.KeyValues = append(export.KeyValues, item)
		}
		if _, _, err := it.Fetch(ctx, false); err != nil && !errors.Is(err, io.EOF) {
			return fmt.Errorf("iterator fetch has failed: %w", err)
		}
		return nil
	}
	if err := kv.WithReader(ctx, memdb, iterate); err != nil {
		return fmt.Errorf("could not iterate over temporary memdb: %w", err)
	}

	// Save the export data into a file.
	fp, err := os.OpenFile(c.outfile, os.O_CREATE|os.O_WRONLY, os.FileMode(0600))
	if err != nil {
		return fmt.Errorf("could not open %q: %w", c.outfile, err)
	}
	defer fp.Close()

	bw := bufio.NewWriter(fp)
	if err := gob.NewEncoder(bw).Encode(export); err != nil {
		return fmt.Errorf("could not gob-encode export data: %w", err)
	}
	if err := bw.Flush(); err != nil {
		return fmt.Errorf("could not flush the bufio writer: %w", err)
	}
	if err := fp.Sync(); err != nil {
		return fmt.Errorf("could not sync the file: %w", err)
	}

	return nil
}

func (c *Export) Synopsis() string {
	return "Saves a trader job state into a file."
}
