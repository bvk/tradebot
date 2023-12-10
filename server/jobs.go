// Copyright (c) 2023 BVK Chaitanya

package server

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path"
	"reflect"

	"github.com/bvk/tradebot/api"
	"github.com/bvk/tradebot/gobs"
	"github.com/bvk/tradebot/job"
	"github.com/bvk/tradebot/kvutil"
	"github.com/bvk/tradebot/namer"
	"github.com/bvk/tradebot/trader"
	"github.com/bvkgo/kv"
	"github.com/google/uuid"
)

func (s *Server) makeJobFunc(v trader.Job) job.Func {
	return func(ctx context.Context) error {
		ename, pid := v.ExchangeName(), v.ProductID()
		product, err := s.getProduct(ctx, ename, pid)
		if err != nil {
			return fmt.Errorf("%s: could not load product %q in exchange %q: %w", v.UID(), pid, ename, err)
		}
		return v.Run(ctx, s.Runtime(product))
	}
}

// createJob creates a job instance for the given trader id. Current state of
// the job is fetched from the database. Returns true if the job requires a
// manual resume request from the user.
func (s *Server) createJob(ctx context.Context, id string) (*job.Job, bool, error) {
	key := path.Join(JobsKeyspace, id)
	gstate, err := kvutil.GetDB[gobs.ServerJobState](ctx, s.db, key)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return nil, false, err
		}
		return nil, false, os.ErrNotExist
	}
	var state job.State
	if gstate.CurrentState != "" {
		state = job.State(gstate.CurrentState)
	}

	var v trader.Job
	v, ok := s.traderMap.Load(id)
	if !ok {
		x, err := loadFromDB(ctx, s.db, id)
		if err != nil {
			return nil, false, fmt.Errorf("job %s not found and could not be loaded: %w", id, err)
		}
		s.traderMap.Store(id, x)
		v = x
	}

	j := job.New(state, s.makeJobFunc(v))
	return j, gstate.NeedsManualResume, nil
}

// doPause pauses a running job. If the job is not running and is not final
// it's state is updated to manually-paused state.
func (s *Server) doPause(ctx context.Context, req *api.JobPauseRequest) (*api.JobPauseResponse, error) {
	j, ok := s.jobMap.Load(req.UID)
	if !ok {
		v, _, err := s.createJob(ctx, req.UID)
		if err != nil {
			return nil, fmt.Errorf("job %s not found: %w", req.UID, os.ErrNotExist)
		}
		j = v
	}

	if job.IsFinal(j.State()) {
		resp := &api.JobPauseResponse{
			FinalState: string(j.State()),
		}
		return resp, nil
	}

	if err := j.Pause(); err != nil {
		return nil, fmt.Errorf("could not pause job %s: %w", req.UID, err)
	}

	gstate := &gobs.ServerJobState{
		CurrentState:      string(j.State()),
		NeedsManualResume: true,
	}
	key := path.Join(JobsKeyspace, req.UID)
	if err := kvutil.SetDB(ctx, s.db, key, gstate); err != nil {
		return nil, err
	}
	s.jobMap.Delete(req.UID)

	resp := &api.JobPauseResponse{
		FinalState: gstate.CurrentState,
	}
	return resp, nil
}

// doResume resumes a non-final job.
func (s *Server) doResume(ctx context.Context, req *api.JobResumeRequest) (*api.JobResumeResponse, error) {
	j, ok := s.jobMap.Load(req.UID)
	if !ok {
		v, _, err := s.createJob(ctx, req.UID)
		if err != nil {
			return nil, fmt.Errorf("job %s not found: %w", req.UID, os.ErrNotExist)
		}
		j = v
	}

	if job.IsFinal(j.State()) {
		resp := &api.JobResumeResponse{
			FinalState: string(j.State()),
		}
		return resp, nil
	}

	if err := j.Resume(s.closeCtx); err != nil {
		return nil, err
	}

	gstate := &gobs.ServerJobState{
		CurrentState:      string(j.State()),
		NeedsManualResume: false,
	}
	key := path.Join(JobsKeyspace, req.UID)
	if err := kvutil.SetDB(ctx, s.db, key, gstate); err != nil {
		return nil, err
	}
	s.jobMap.Store(req.UID, j)

	resp := &api.JobResumeResponse{
		FinalState: gstate.CurrentState,
	}
	return resp, nil
}

// doCancel cancels a non-final job. If job is running, it will be stopped.
func (s *Server) doCancel(ctx context.Context, req *api.JobCancelRequest) (*api.JobCancelResponse, error) {
	j, ok := s.jobMap.Load(req.UID)
	if !ok {
		v, _, err := s.createJob(ctx, req.UID)
		if err != nil {
			return nil, fmt.Errorf("job %s not found: %w", req.UID, os.ErrNotExist)
		}
		j = v
	}

	if job.IsFinal(j.State()) {
		resp := &api.JobCancelResponse{
			FinalState: string(j.State()),
		}
		return resp, nil
	}

	if err := j.Cancel(); err != nil {
		return nil, err
	}

	gstate := &gobs.ServerJobState{
		CurrentState: string(j.State()),
	}
	key := path.Join(JobsKeyspace, req.UID)
	if err := kvutil.SetDB(ctx, s.db, key, gstate); err != nil {
		return nil, err
	}
	s.jobMap.Delete(req.UID)

	resp := &api.JobCancelResponse{
		FinalState: gstate.CurrentState,
	}
	return resp, nil
}

func (s *Server) doList(ctx context.Context, req *api.JobListRequest) (*api.JobListResponse, error) {
	getState := func(id string) job.State {
		if j, ok := s.jobMap.Load(id); ok {
			return j.State()
		}
		key := path.Join(JobsKeyspace, id)
		v, err := kvutil.GetDB[gobs.ServerJobState](ctx, s.db, key)
		if err != nil {
			log.Printf("could not fetch job state for %s (ignored): %v", id, err)
			return ""
		}
		return job.State(v.CurrentState)
	}

	snap, err := s.db.NewSnapshot(ctx)
	if err != nil {
		return nil, fmt.Errorf("could not create a db snapshot: %w", err)
	}
	defer snap.Discard(ctx)

	resp := new(api.JobListResponse)
	s.traderMap.Range(func(id string, v trader.Job) bool {
		name, _, _ := namer.ResolveID(ctx, snap, id)
		resp.Jobs = append(resp.Jobs, &api.JobListResponseItem{
			UID:   v.UID(),
			Type:  reflect.TypeOf(v).Elem().Name(),
			State: string(getState(id)),
			Name:  name,
		})
		return true
	})
	return resp, nil
}

func (s *Server) doSetJobName(ctx context.Context, req *api.SetJobNameRequest) (*api.SetJobNameResponse, error) {
	if err := req.Check(); err != nil {
		return nil, fmt.Errorf("invalid rename request: %w", err)
	}

	if _, err := uuid.Parse(req.UID); err != nil {
		return nil, fmt.Errorf("job uid must be an uuid: %w", err)
	}

	setName := func(ctx context.Context, rw kv.ReadWriter) error {
		key := path.Join(JobsKeyspace, req.UID)
		if _, err := kvutil.Get[gobs.ServerJobState](ctx, rw, key); err != nil {
			return fmt.Errorf("could not fetch job state: %w", err)
		}

		job, ok := s.traderMap.Load(req.UID)
		if !ok {
			x, err := Load(ctx, rw, req.UID)
			if err != nil {
				return fmt.Errorf("job %s not found and could not be loaded: %w", req.UID, err)
			}
			s.traderMap.Store(req.UID, x)
			job = x
		}

		typename := reflect.TypeOf(job).Elem().Name()
		if err := namer.SetName(ctx, rw, req.UID, req.JobName, typename); err != nil {
			return fmt.Errorf("could not set job name: %w", err)
		}
		return nil
	}
	if err := kv.WithReadWriter(ctx, s.db, setName); err != nil {
		return nil, err
	}

	return &api.SetJobNameResponse{}, nil
}
