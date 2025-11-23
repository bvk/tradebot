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

// Import imports new job from an export. If writer is nil, then a new
// transaction will be used for the database updates.
func (r *Runner) Import(ctx context.Context, writer kv.ReadWriter, export *gobs.JobExportData) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if export.JobState == gobs.RUNNING {
		return fmt.Errorf("jobs with RUNNING state cannot be imported")
	}

	jd := &gobs.JobData{
		ID:       export.UID,
		Typename: export.Typename,
		Flags:    export.JobFlags,
		State:    export.JobState,
	}
	key := path.Join(Keyspace, export.UID)
	if err := kvutil.Set(ctx, writer, key, jd); err != nil {
		return err
	}
	return nil
}

// Export current state of a job to an export. If reader is nil, then a new
// snapshot will be used for reading the database.
func (r *Runner) Export(ctx context.Context, reader kv.Reader, uid string, export *gobs.JobExportData) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.jobMap[uid]; ok {
		return fmt.Errorf("running job cannot be exported")
	}

	key := path.Join(Keyspace, uid)
	jd, err := kvutil.Get[gobs.JobData](ctx, reader, key)
	if err != nil {
		return fmt.Errorf("could not read job data from db: %w", err)
	}

	export.JobFlags = jd.Flags
	export.Typename = jd.Typename
	export.JobState = jd.State
	return nil
}

// Status returns the current expected status of a job as per the database.
func Status(ctx context.Context, r kv.Reader, uid string) (gobs.State, error) {
	key := path.Join(Keyspace, uid)
	gob, err := kvutil.Get[gobs.JobData](ctx, r, key)
	if err != nil {
		return "", fmt.Errorf("could not read job data from db: %w", err)
	}
	if gob.State == "" {
		return gobs.PAUSED, nil
	}
	return gob.State, nil
}

// StatusDB returns the status of a job in the database by job's uid.
func StatusDB(ctx context.Context, db kv.Database, uid string) (state gobs.State, err error) {
	kv.WithReader(ctx, db, func(ctx context.Context, r kv.Reader) error {
		state, err = Status(ctx, r, uid)
		return nil
	})
	return state, err
}
