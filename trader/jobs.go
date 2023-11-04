// Copyright (c) 2023 BVK Chaitanya

package trader

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path"

	"github.com/bvkgo/kv"
	"github.com/bvkgo/tradebot/api"
	"github.com/bvkgo/tradebot/dbutil"
	"github.com/bvkgo/tradebot/exchange"
	"github.com/bvkgo/tradebot/job"
	"github.com/bvkgo/tradebot/limiter"
	"github.com/bvkgo/tradebot/looper"
	"github.com/bvkgo/tradebot/waller"
)

// createJob creates a job instance for the given trader id. Current state of
// the job is fetched from the database. Returns true if the job requires a
// manual resume request from the user.
func (t *Trader) createJob(ctx context.Context, id string) (*job.Job, bool, error) {
	key := path.Join(JobsKeyspace, id)
	gstate, err := dbutil.Get[gobJobState](ctx, t.db, key)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return nil, false, err
		}
	}

	var pid string
	var run func(context.Context, exchange.Product, kv.Database) error

	if limit, ok := t.limiterMap.Load(id); ok {
		pid, run = limit.Status().ProductID, limit.Run
	} else if loop, ok := t.looperMap.Load(id); ok {
		pid, run = loop.Status().ProductID, loop.Run
	} else if wall, ok := t.wallerMap.Load(id); ok {
		pid, run = wall.Status().ProductID, wall.Run
	} else {
		return nil, false, fmt.Errorf("job %s not found: %w", os.ErrNotExist)
	}

	product, ok := t.productMap[pid]
	if !ok {
		return nil, false, fmt.Errorf("job %s product %q is not enabled (ignored)", id, pid)
	}

	j := job.New(gstate.State, func(ctx context.Context) error {
		return run(ctx, product, t.db)
	})
	return j, gstate.NeedsManualResume, nil
}

// doPause pauses a running job. If the job is not running and is not final
// it's state is updated to manually-paused state.
func (t *Trader) doPause(ctx context.Context, req *api.PauseRequest) (*api.PauseResponse, error) {
	j, ok := t.jobMap.Load(req.UID)
	if !ok {
		v, _, err := t.createJob(ctx, req.UID)
		if err != nil {
			return nil, fmt.Errorf("job %s not found: %w", req.UID, os.ErrNotExist)
		}
		j = v
	}

	if job.IsFinal(j.State()) {
		resp := &api.PauseResponse{
			FinalState: string(j.State()),
		}
		return resp, nil
	}

	if err := j.Pause(); err != nil {
		return nil, fmt.Errorf("could not pause job %s: %w", req.UID, err)
	}

	gstate := &gobJobState{
		State:             j.State(),
		NeedsManualResume: true,
	}
	key := path.Join(JobsKeyspace, req.UID)
	if err := dbutil.Set(ctx, t.db, key, gstate); err != nil {
		return nil, err
	}
	t.jobMap.Delete(req.UID)

	resp := &api.PauseResponse{
		FinalState: string(gstate.State),
	}
	return resp, nil
}

// doResume resumes a non-final job.
func (t *Trader) doResume(ctx context.Context, req *api.ResumeRequest) (*api.ResumeResponse, error) {
	j, ok := t.jobMap.Load(req.UID)
	if !ok {
		v, _, err := t.createJob(ctx, req.UID)
		if err != nil {
			return nil, fmt.Errorf("job %s not found: %w", req.UID, os.ErrNotExist)
		}
		j = v
	}

	if job.IsFinal(j.State()) {
		resp := &api.ResumeResponse{
			FinalState: string(j.State()),
		}
		return resp, nil
	}

	if err := j.Resume(t.closeCtx); err != nil {
		return nil, err
	}

	gstate := &gobJobState{
		State:             j.State(),
		NeedsManualResume: false,
	}
	key := path.Join(JobsKeyspace, req.UID)
	if err := dbutil.Set(ctx, t.db, key, gstate); err != nil {
		return nil, err
	}
	t.jobMap.Store(req.UID, j)

	resp := &api.ResumeResponse{
		FinalState: string(gstate.State),
	}
	return resp, nil
}

// doCancel cancels a non-final job. If job is running, it will be stopped.
func (t *Trader) doCancel(ctx context.Context, req *api.CancelRequest) (*api.CancelResponse, error) {
	j, ok := t.jobMap.Load(req.UID)
	if !ok {
		v, _, err := t.createJob(ctx, req.UID)
		if err != nil {
			return nil, fmt.Errorf("job %s not found: %w", req.UID, os.ErrNotExist)
		}
		j = v
	}

	if job.IsFinal(j.State()) {
		resp := &api.CancelResponse{
			FinalState: string(j.State()),
		}
		return resp, nil
	}

	if err := j.Cancel(); err != nil {
		return nil, err
	}

	gstate := &gobJobState{
		State: j.State(),
	}
	key := path.Join(JobsKeyspace, req.UID)
	if err := dbutil.Set(ctx, t.db, key, gstate); err != nil {
		return nil, err
	}
	t.jobMap.Delete(req.UID)

	resp := &api.CancelResponse{
		FinalState: string(gstate.State),
	}
	return resp, nil
}

func (t *Trader) doList(ctx context.Context, req *api.ListRequest) (*api.ListResponse, error) {
	getState := func(id string) job.State {
		if j, ok := t.jobMap.Load(id); ok {
			return j.State()
		}
		key := path.Join(JobsKeyspace, id)
		v, err := dbutil.Get[gobJobState](ctx, t.db, key)
		if err != nil {
			log.Printf("could not fetch job state for %s (ignored): %v", id, err)
			return ""
		}
		return v.State
	}

	resp := new(api.ListResponse)
	t.limiterMap.Range(func(id string, l *limiter.Limiter) bool {
		resp.Jobs = append(resp.Jobs, &api.ListResponseItem{
			UID:   id,
			Type:  "Limiter",
			State: string(getState(id)),
		})
		return true
	})
	t.looperMap.Range(func(id string, l *looper.Looper) bool {
		resp.Jobs = append(resp.Jobs, &api.ListResponseItem{
			UID:   id,
			Type:  "Looper",
			State: string(getState(id)),
		})
		return true
	})
	t.wallerMap.Range(func(id string, w *waller.Waller) bool {
		resp.Jobs = append(resp.Jobs, &api.ListResponseItem{
			UID:   id,
			Type:  "Waller",
			State: string(getState(id)),
		})
		return true
	})
	return resp, nil
}
