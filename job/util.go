// Copyright (c) 2023 BVK Chaitanya

package job

import (
	"context"
	"fmt"
	"path"

	"github.com/bvk/tradebot/gobs"
	"github.com/bvk/tradebot/kvutil"
	"github.com/bvkgo/kv"
)

func ResumeDB(ctx context.Context, r *Runner, db kv.Database, uid string, fn Func, fctx context.Context) (state State, err error) {
	kv.WithReadWriter(ctx, db, func(ctx context.Context, rw kv.ReadWriter) error {
		state, err = r.Resume(ctx, rw, uid, fn, fctx)
		return nil
	})
	return state, err
}

func PauseDB(ctx context.Context, r *Runner, db kv.Database, uid string) (state State, err error) {
	kv.WithReadWriter(ctx, db, func(ctx context.Context, rw kv.ReadWriter) error {
		state, err = r.Pause(ctx, rw, uid)
		return nil
	})
	return state, err
}

func CancelDB(ctx context.Context, r *Runner, db kv.Database, uid string) (state State, err error) {
	kv.WithReadWriter(ctx, db, func(ctx context.Context, rw kv.ReadWriter) error {
		state, err = r.Cancel(ctx, rw, uid)
		return nil
	})
	return state, err
}

func ScanDB(ctx context.Context, r *Runner, db kv.Database, fn func(context.Context, kv.Reader, *JobData) error) error {
	return kv.WithReader(ctx, db, func(ctx context.Context, reader kv.Reader) error {
		return r.Scan(ctx, reader, fn)
	})
}

func StopAllDB(ctx context.Context, r *Runner, db kv.Database) error {
	return kv.WithReadWriter(ctx, db, func(ctx context.Context, rw kv.ReadWriter) error {
		return r.StopAll(ctx, rw)
	})
}

func (r *Runner) Import(ctx context.Context, writer kv.ReadWriter, export *gobs.JobExportData) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	jd := &JobData{
		UID:      export.UID,
		Typename: export.Typename,
		Flags:    export.JobFlags,
		State:    State(export.JobState),
	}
	return r.setLocked(ctx, writer, export.UID, jd)
}

func (r *Runner) Export(ctx context.Context, reader kv.Reader, export *gobs.JobExportData) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	jd, err := r.getLocked(ctx, reader, export.UID)
	if err != nil {
		return err
	}

	if len(export.Typename) > 0 && export.Typename != jd.Typename {
		return fmt.Errorf("type mismatch (want %q, has %q)", jd.Typename, export.Typename)
	}
	export.JobFlags = jd.Flags
	export.Typename = jd.Typename
	export.JobState = string(jd.State)
	return nil
}

func Status(ctx context.Context, r kv.Reader, uid string) (State, error) {
	key := path.Join(Keyspace, uid)
	gob, err := kvutil.Get[gobs.JobData](ctx, r, key)
	if err != nil {
		return "", fmt.Errorf("could not read job data from db: %w", err)
	}
	if gob.State == "" {
		return PAUSED, nil
	}
	return State(gob.State), nil
}

func StatusDB(ctx context.Context, db kv.Database, uid string) (state State, err error) {
	kv.WithReader(ctx, db, func(ctx context.Context, r kv.Reader) error {
		state, err = Status(ctx, r, uid)
		return nil
	})
	return state, err
}
