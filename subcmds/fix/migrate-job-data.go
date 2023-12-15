// Copyright (c) 2023 BVK Chaitanya

package fix

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"reflect"
	"strings"

	"github.com/bvk/tradebot/cli"
	"github.com/bvk/tradebot/gobs"
	"github.com/bvk/tradebot/job"
	"github.com/bvk/tradebot/kvutil"
	"github.com/bvk/tradebot/server"
	"github.com/bvk/tradebot/subcmds/cmdutil"
	"github.com/bvkgo/kv"
)

type MigrateJobData struct {
	cmdutil.DBFlags

	dryRun bool
}

func (c *MigrateJobData) Command() (*flag.FlagSet, cli.CmdFunc) {
	fset := flag.NewFlagSet("migrate-job-data", flag.ContinueOnError)
	c.DBFlags.SetFlags(fset)
	fset.BoolVar(&c.dryRun, "dry-run", true, "when true only prints the information")
	return fset, cli.CmdFunc(c.run)
}

func toJobData(s *gobs.ServerJobState, uid, typename string) *gobs.JobData {
	var flags uint64
	if s.NeedsManualResume {
		flags |= server.ManualFlag
	}
	state := job.PAUSED
	switch {
	case s.CurrentState == "" || s.CurrentState == "PAUSED":
		state = job.PAUSED
	case s.CurrentState == "RESUMED":
		state = job.RUNNING
	case s.CurrentState == "CANCELED":
		state = job.CANCELED
	case s.CurrentState == "COMPLETED":
		state = job.COMPLETED
	case strings.HasPrefix(s.CurrentState, "FAILED:"):
		state = job.FAILED
	default:
		return nil
	}
	return &gobs.JobData{
		ID:       uid,
		Typename: typename,
		Flags:    flags,
		State:    string(state),
	}
}

func (c *MigrateJobData) run(ctx context.Context, args []string) error {
	if len(args) != 0 {
		return fmt.Errorf("this command takes no arguments")
	}

	db, closer, err := c.DBFlags.GetDatabase(ctx)
	if err != nil {
		return err
	}
	defer closer()

	migrate := func(ctx context.Context, rw kv.ReadWriter) error {
		begin, end := kvutil.PathRange(job.Keyspace)
		it, err := rw.Ascend(ctx, begin, end)
		if err != nil {
			return err
		}
		defer kv.Close(it)

		for k, _, err := it.Fetch(ctx, false); err == nil; k, _, err = it.Fetch(ctx, true) {
			old, err := kvutil.Get[gobs.ServerJobState](ctx, rw, k)
			if err != nil {
				return fmt.Errorf("could not load old state at %q: %w", k, err)
			}
			uid := strings.TrimPrefix(k, job.Keyspace)
			trader, err := server.Load(ctx, rw, uid)
			if err != nil {
				return fmt.Errorf("could not load trader %q: %w", uid, err)
			}
			typename := reflect.ValueOf(trader).Elem().Type().Name()
			jd := toJobData(old, uid, typename)
			if jd == nil {
				return fmt.Errorf("could not convert %#v to job-data", old)
			}
			log.Printf("%s %s %#v -> %#v", k, typename, old, jd)
			if !c.dryRun {
				if err := kvutil.Set(ctx, rw, k, jd); err != nil {
					return fmt.Errorf("could not migrate %q to job-data: %w", k, err)
				}
			}
		}
		if _, _, err := it.Fetch(ctx, false); err != nil && !errors.Is(err, io.EOF) {
			return err
		}
		return nil
	}
	if err := kv.WithReadWriter(ctx, db, migrate); err != nil {
		return err
	}
	return nil
}
