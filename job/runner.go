// Copyright (c) 2023 BVK Chaitanya

package job

import (
	"context"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"maps"
	"os"
	"path"
	"slices"
	"sync"

	"github.com/bvk/tradebot/gobs"
	"github.com/bvk/tradebot/kvutil"
	"github.com/bvkgo/kv"
)

const Keyspace = "/jobs/"

type Runner struct {
	mu sync.Mutex

	db kv.Database

	// jobMap holds running jobs.
	jobMap map[string]*Job
}

// NewRunner creates a job runner that uses the input database for persistency.
func NewRunner(db kv.Database) *Runner {
	return &Runner{
		db:     db,
		jobMap: make(map[string]*Job),
	}
}

// PauseAll pauses all running jobs and waits for the goroutines to complete or
// the input context to expire.
func (r *Runner) PauseAll(ctx context.Context) error {
	r.mu.Lock()
	jobs := slices.Collect(maps.Values(r.jobMap))
	r.mu.Unlock()

	for _, v := range jobs {
		v.Pause()
	}

	for _, v := range jobs {
		if err := v.Wait(ctx); err != nil {
			return err
		}
	}
	return nil
}

// Resume starts a paused job.
func (r *Runner) Resume(ctx context.Context, uid string, fn Func, fctx context.Context) error {
	wrapper := func(ctx context.Context) error {
		// Run the input function as the job.
		status := fn(ctx)
		log.Printf("job %q has returned with status: %v", uid, status)

		// Update the job state as FAILED/COMPLETE/PAUSED in the database. If the
		// database update fails, we will end up with persistent state as RUNNING
		// but with the user-level function finished, in which case, we won't
		// remove it from the running jobMap, so that it is still running.

		tx, err := r.db.NewTransaction(ctx)
		if err != nil {
			return err
		}
		defer tx.Rollback(ctx)

		key := path.Join(Keyspace, uid)
		jd, err := kvutil.Get[gobs.JobData](ctx, tx, key)
		if err != nil {
			return err
		}

		switch {
		default:
			jd.State = gobs.FAILED
		case status == nil:
			jd.State = gobs.COMPLETED
		case errors.Is(status, errPause):
			jd.State = gobs.PAUSED
		case errors.Is(status, errCancel):
			jd.State = gobs.CANCELED

		case errors.Is(status, errDone):
			slog.Error("unexpected: private errDone return status from the user function", "err", err)
			jd.State = gobs.COMPLETED
		}

		if err := kvutil.Set(ctx, tx, key, jd); err != nil {
			return err
		}

		r.mu.Lock()
		defer r.mu.Unlock()

		if err := tx.Commit(ctx); err != nil {
			return err
		}

		// Remove the job only after its final state is successfully written to
		// database.
		delete(r.jobMap, uid)
		return errDone
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.jobMap[uid]; ok {
		return fmt.Errorf("job %q is already running", uid)
	}

	// Update job as RUNNING in the database. We don't begin the job if this
	// database update fails, so that DB state and runtime state are consistent.
	tx, err := r.db.NewTransaction(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	key := path.Join(Keyspace, uid)
	jd, err := kvutil.Get[gobs.JobData](ctx, tx, key)
	if err != nil {
		return fmt.Errorf("could not read job data from db: %w", err)
	}

	if jd.State.IsDone() {
		return fmt.Errorf("job %q is already complete", uid)
	}

	jd.State = gobs.RUNNING
	if err := kvutil.Set(ctx, tx, key, jd); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}

	r.jobMap[uid] = Run(wrapper, fctx)
	return nil
}

// Get returns an existing job's information.
func (r *Runner) Get(ctx context.Context, reader kv.Reader, uid string) (*gobs.JobData, error) {
	if reader == nil {
		snap, err := r.db.NewSnapshot(ctx)
		if err != nil {
			return nil, err
		}
		defer snap.Discard(ctx)

		reader = snap
	}

	key := path.Join(Keyspace, uid)
	jd, err := kvutil.Get[gobs.JobData](ctx, reader, key)
	if err != nil {
		return nil, fmt.Errorf("could not read job data from db: %w", err)
	}
	return jd, nil
}

// UpdateFlags updates the user-defined flags of an existing job.
func (r *Runner) UpdateFlags(ctx context.Context, writer kv.ReadWriter, uid string, flags uint64) (status error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	rw := writer
	var tx kv.Transaction
	if writer == nil {
		tmp, err := r.db.NewTransaction(ctx)
		if err != nil {
			return err
		}
		defer tx.Rollback(ctx)

		rw, tx = tmp, tmp
	}

	key := path.Join(Keyspace, uid)
	jd, err := kvutil.Get[gobs.JobData](ctx, rw, key)
	if err != nil {
		return fmt.Errorf("could not read job data from db: %w", err)
	}

	jd.Flags = flags
	if err := kvutil.Set(ctx, rw, key, jd); err != nil {
		return fmt.Errorf("could not update metadata for job %q: %w", uid, err)
	}

	if writer == nil {
		if err := tx.Commit(ctx); err != nil {
			return err
		}
	}
	return nil
}

// Add creates a new job in the database. Jobs are created in PAUSED state and
// must be resumed to begin execution. If writer is non-nil, then job is added
// to the database within the input writer's transaction and database update
// depends on the result of the writer transaction's commit operation.
func (r *Runner) Add(ctx context.Context, writer kv.ReadWriter, uid, typename string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.jobMap[uid]; ok {
		return fmt.Errorf("job with uid %s already exists and is running: %w", uid, os.ErrExist)
	}

	// create a local transaction if necessary.
	rw := writer
	var tx kv.Transaction
	if writer == nil {
		tmp, err := r.db.NewTransaction(ctx)
		if err != nil {
			return err
		}
		defer tmp.Rollback(ctx)

		tx, rw = tmp, tmp
	}

	key := path.Join(Keyspace, uid)
	if _, err := kvutil.Get[gobs.JobData](ctx, rw, key); err == nil || !errors.Is(err, os.ErrNotExist) {
		if err == nil {
			return fmt.Errorf("job with uid already exists: %w", os.ErrExist)
		}
		return fmt.Errorf("could not read job data from db: %w", err)
	}

	jd := &gobs.JobData{
		ID:       uid,
		Typename: typename,
		State:    gobs.PAUSED,
	}
	if err := kvutil.Set(ctx, rw, key, jd); err != nil {
		return err
	}

	if writer == nil {
		if err := tx.Commit(ctx); err != nil {
			return err
		}
	}
	return nil
}

// Remove deletes a job from the database. Running jobs cannot be removed. If
// writer is non-nil, then removal depends on the result of input writer's
// commit operation.
func (r *Runner) Remove(ctx context.Context, writer kv.ReadWriter, uid string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.jobMap[uid]; ok {
		return fmt.Errorf("running job %q cannot be removed", uid)
	}

	rw := writer
	var tx kv.Transaction
	if writer == nil {
		tmp, err := r.db.NewTransaction(ctx)
		if err != nil {
			return err
		}
		defer tmp.Rollback(ctx)

		tx, rw = tmp, tmp
	}

	key := path.Join(Keyspace, uid)
	if err := rw.Delete(ctx, key); err != nil {
		return fmt.Errorf("could not delete key %q: %w", key, err)
	}

	if writer == nil {
		if err := tx.Commit(ctx); err != nil {
			return err
		}
	}
	return nil
}

// Scan invokes the callback function with all jobs defined in the database. If
// the reader is nil, then a new snapshot will be used for reading the
// database.
func (r *Runner) Scan(ctx context.Context, reader kv.Reader, fn func(ctx context.Context, r kv.Reader, item *gobs.JobData) error) error {
	if reader == nil {
		snap, err := r.db.NewSnapshot(ctx)
		if err != nil {
			return err
		}
		defer snap.Discard(ctx)

		reader = snap
	}

	// Database state is the most up to date even for the running jobs.

	begin, end := kvutil.PathRange(Keyspace)
	cb := func(ctx context.Context, reader kv.Reader, key string, value *gobs.JobData) error {
		return fn(ctx, reader, value)
	}
	return kvutil.Ascend(ctx, reader, begin, end, cb)
}

// Pause stops a running job. Paused jobs can be resumed later.
func (r *Runner) Pause(ctx context.Context, uid string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	job, ok := r.jobMap[uid]
	if ok {
		// Wrapper function needs to acquire the lock before the goroutine completes.
		r.mu.Unlock()
		defer r.mu.Lock()

		job.Pause()
		if err := job.Wait(ctx); err != nil {
			return err
		}
		return nil
	}

	// Job is not running. Update the State to PAUSED in the database.

	tx, err := r.db.NewTransaction(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	key := path.Join(Keyspace, uid)
	jd, err := kvutil.Get[gobs.JobData](ctx, tx, key)
	if err != nil {
		return fmt.Errorf("could not read job data from db: %w", err)
	}

	if jd.State.IsDone() {
		return fmt.Errorf("cannot pause an already completed job")
	}

	jd.State = gobs.PAUSED
	if err := kvutil.Set(ctx, tx, key, jd); err != nil {
		return fmt.Errorf("could not mark job %q as paused: %w", uid, err)
	}

	if err := tx.Commit(ctx); err != nil {
		return err
	}
	return nil
}

// Cancel forces a job to it's finished state. If a job is running or not, it
// will be marked as canceled. Canceled jobs cannot be resumed later.
func (r *Runner) Cancel(ctx context.Context, uid string) (*gobs.JobData, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Running jobs.
	if job, ok := r.jobMap[uid]; ok {
		job.Cancel()

		r.mu.Unlock()
		defer r.mu.Lock()

		if err := job.Wait(ctx); err != nil {
			return nil, err
		}
		return r.Get(ctx, nil, uid)
	}

	tx, err := r.db.NewTransaction(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	key := path.Join(Keyspace, uid)
	jd, err := kvutil.Get[gobs.JobData](ctx, tx, key)
	if err != nil {
		return nil, fmt.Errorf("could not read job data from db: %w", err)
	}

	if jd.State.IsDone() {
		return jd, nil
	}

	jd.State = gobs.CANCELED
	if err := kvutil.Set(ctx, tx, key, jd); err != nil {
		return nil, fmt.Errorf("could not mark job %q as canceled: %w", uid, err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return jd, nil
}
