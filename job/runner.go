// Copyright (c) 2023 BVK Chaitanya

package job

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path"
	"strings"
	"sync"

	"github.com/bvk/tradebot/gobs"
	"github.com/bvk/tradebot/kvutil"
	"github.com/bvkgo/kv"
)

const Keyspace = "/jobs/"

type State string

const (
	PAUSED    State = "PAUSED"
	RUNNING   State = "RUNNING"
	COMPLETED State = "COMPLETED"
	CANCELED  State = "CANCELED"
	FAILED    State = "FAILED"
)

func IsStopped(s State) bool {
	return s != RUNNING
}

func IsDone(s State) bool {
	return s == COMPLETED || s == CANCELED || s == FAILED
}

type JobData struct {
	UID      string
	Typename string
	Flags    uint64

	State State
}

func toGob(v *JobData) *gobs.JobData {
	if v.State == "" {
		v.State = PAUSED
	}
	return &gobs.JobData{
		ID:       v.UID,
		Typename: v.Typename,
		Flags:    v.Flags,
		State:    string(v.State),
	}
}

func fromGob(v *gobs.JobData) *JobData {
	if v.State == "" {
		v.State = string(PAUSED)
	}
	return &JobData{
		UID:      v.ID,
		Typename: v.Typename,
		Flags:    v.Flags,
		State:    State(v.State),
	}
}

type Runner struct {
	mu sync.Mutex

	// jobMap holds metadata for all running jobs.
	jobMap map[string]*Job

	// dataMap holds metadata for all running jobs and also more jobs, like
	// completed, canceled, etc. Metadata in this map is always newer than the
	// metadata in the database. This in-memory map is necessary cause runner's
	// wrapJobFunc has no db access (to save the completed state) when the job is
	// completed.
	dataMap map[string]*JobData
}

func NewRunner() *Runner {
	return &Runner{
		jobMap:  make(map[string]*Job),
		dataMap: make(map[string]*JobData),
	}
}

func (r *Runner) syncLocked(ctx context.Context, rw kv.ReadWriter) error {
	for uid, jd := range r.dataMap {
		if err := r.setLocked(ctx, rw, uid, jd); err != nil {
			return fmt.Errorf("could not sync metadata for job %q: %w", uid, err)
		}
	}
	return nil
}

func (r *Runner) StopAll(ctx context.Context, rw kv.ReadWriter) error {
	var jobs []*Job

	r.mu.Lock()
	for uid, job := range r.jobMap {
		job.Pause()
		delete(r.jobMap, uid)
		jobs = append(jobs, job)
	}
	r.mu.Unlock()

	for _, job := range jobs {
		job.Wait()
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	return r.syncLocked(ctx, rw)
}

func (r *Runner) wrapJobFunc(uid string, fn Func) Func {
	return func(ctx context.Context) error {
		status := fn(ctx)
		log.Printf("job %q has returned with status: %v", uid, status)

		r.mu.Lock()
		defer r.mu.Unlock()

		if job, ok := r.jobMap[uid]; ok {
			data := r.dataMap[uid]
			data.State = job.State()

			delete(r.jobMap, uid)
		}

		return status
	}
}

// Get returns a job's information.
func (r *Runner) Get(ctx context.Context, reader kv.Reader, uid string) (*JobData, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	jd, err := r.getLocked(ctx, reader, uid)
	if err != nil {
		return nil, fmt.Errorf("could not load job data: %w", err)
	}
	return jd, nil
}

func (r *Runner) getLocked(ctx context.Context, reader kv.Reader, uid string) (*JobData, error) {
	jd, ok := r.dataMap[uid]
	if ok {
		if job, ok := r.jobMap[uid]; ok {
			jd.State = job.State()
		}
		return jd, nil
	}

	key := path.Join(Keyspace, uid)
	gob, err := kvutil.Get[gobs.JobData](ctx, reader, key)
	if err != nil {
		return nil, fmt.Errorf("could not read job data from db: %w", err)
	}

	jd = fromGob(gob)
	r.dataMap[uid] = jd
	return jd, nil
}

func (r *Runner) setLocked(ctx context.Context, writer kv.ReadWriter, uid string, jd *JobData) error {
	key := path.Join(Keyspace, uid)
	if err := kvutil.Set(ctx, writer, key, toGob(jd)); err != nil {
		return fmt.Errorf("could not update metadata for job %q: %w", uid, err)
	}
	// Database is synced with the latest version, so we can drop the in-memory
	// data for non-running jobs.
	if _, ok := r.jobMap[uid]; !ok {
		delete(r.dataMap, uid)
	}
	return nil
}

func (r *Runner) UpdateFlags(ctx context.Context, rw kv.ReadWriter, uid string, flags uint64) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	jd, err := r.getLocked(ctx, rw, uid)
	if err != nil {
		return fmt.Errorf("could not load job data: %w", err)
	}
	jd.Flags = flags
	if err := r.setLocked(ctx, rw, uid, jd); err != nil {
		return fmt.Errorf("could not update flags: %w", err)
	}
	return nil
}

// Add creates a new job in the database. Jobs are created in PAUSED state and
// must be resumed to begin execution.
func (r *Runner) Add(ctx context.Context, writer kv.ReadWriter, uid, typename string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, err := r.getLocked(ctx, writer, uid); err == nil || !errors.Is(err, os.ErrNotExist) {
		if err == nil {
			return fmt.Errorf("job with uid already exists: %w", os.ErrExist)
		}
		return fmt.Errorf("could not check if uid already exists: %w", err)
	}

	jd := &JobData{
		UID:      uid,
		Typename: typename,
		State:    PAUSED,
	}
	if err := r.setLocked(ctx, writer, uid, jd); err != nil {
		return fmt.Errorf("could not save new job entry: %w", err)
	}
	return nil
}

// Remove deletes a job from the database. Running jobs cannot be removed.
func (r *Runner) Remove(ctx context.Context, writer kv.ReadWriter, uid string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.jobMap[uid]; ok {
		return fmt.Errorf("running job %q cannot be removed", uid)
	}

	key := path.Join(Keyspace, uid)
	if err := writer.Delete(ctx, key); err != nil {
		return fmt.Errorf("could not delete key %q: %w", key, err)
	}
	delete(r.dataMap, uid)
	return nil
}

// Scan invokes the callback function with all jobs defined in the database.
func (r *Runner) Scan(ctx context.Context, reader kv.Reader, fn func(ctx context.Context, r kv.Reader, item *JobData) error) error {
	begin, end := kvutil.PathRange(Keyspace)
	cb := func(ctx context.Context, _ kv.Reader, key string, value *gobs.JobData) error {
		uid := strings.TrimPrefix(key, Keyspace)

		r.mu.Lock()
		jd, ok := r.dataMap[uid]
		if ok {
			if job, ok := r.jobMap[uid]; ok {
				jd.State = job.State()
			}
		}
		r.mu.Unlock()

		if jd == nil {
			jd = fromGob(value)
		}
		return fn(ctx, reader, jd)
	}
	return kvutil.Ascend(ctx, reader, begin, end, cb)
}

// Resume runs a job. Job must be in PAUSED state.
func (r *Runner) Resume(ctx context.Context, writer kv.ReadWriter, uid string, fn Func, fctx context.Context) (State, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.jobMap[uid]; ok {
		return "", fmt.Errorf("job %q is already resumed: %w", uid, os.ErrExist)
	}

	jd, err := r.getLocked(ctx, writer, uid)
	if err != nil {
		return "", fmt.Errorf("could not load job data for %q: %w", uid, err)
	}

	if IsDone(jd.State) {
		return "", fmt.Errorf("job %q is already completed", uid)
	}

	job := Run(r.wrapJobFunc(uid, fn), fctx)
	r.jobMap[uid] = job

	jd.State = job.State()
	if err := r.setLocked(ctx, writer, uid, jd); err != nil {
		log.Printf("could not update job state in the db (ignored): %v", err)
	}
	return jd.State, nil
}

// Pause stops a running job. Job can be resumed later.
func (r *Runner) Pause(ctx context.Context, writer kv.ReadWriter, uid string) (State, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if job, ok := r.jobMap[uid]; ok {
		job.Pause()
		r.mu.Unlock()
		job.Wait()
		r.mu.Lock()
	}

	jd, err := r.getLocked(ctx, writer, uid)
	if err != nil {
		return "", fmt.Errorf("could not load job state: %w", err)
	}
	if !IsDone(jd.State) {
		jd.State = PAUSED
	}
	if err := r.setLocked(ctx, writer, uid, jd); err != nil {
		return "", fmt.Errorf("could not mark job %q as paused: %w", uid, err)
	}
	return jd.State, nil
}

// Cancel stops the job if it is running and marks it as canceled. Job cannot
// be resumed after it is canceled.
func (r *Runner) Cancel(ctx context.Context, writer kv.ReadWriter, uid string) (State, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if job, ok := r.jobMap[uid]; ok {
		job.Cancel()
		r.mu.Unlock()
		job.Wait()
		r.mu.Lock()
	}

	jd, err := r.getLocked(ctx, writer, uid)
	if err != nil {
		return "", fmt.Errorf("could not load job state: %w", err)
	}
	if !IsDone(jd.State) {
		jd.State = CANCELED
	}
	if err := r.setLocked(ctx, writer, uid, jd); err != nil {
		return "", fmt.Errorf("could not mark job %q as canceled: %w", uid, err)
	}
	return jd.State, nil
}
