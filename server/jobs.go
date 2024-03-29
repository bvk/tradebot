// Copyright (c) 2023 BVK Chaitanya

package server

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"

	"github.com/bvk/tradebot/api"
	"github.com/bvk/tradebot/job"
	"github.com/bvk/tradebot/namer"
	"github.com/bvk/tradebot/trader"
	"github.com/bvkgo/kv"
	"github.com/google/uuid"
)

const (
	ManualFlag uint64 = 0x1 << 0
)

func (s *Server) makeJobFunc(v trader.Trader) job.Func {
	return func(ctx context.Context) error {
		uid := v.UID()

		ename, pid := v.ExchangeName(), v.ProductID()
		product, err := s.getProduct(ctx, ename, pid)
		if err != nil {
			return fmt.Errorf("%s: could not load product %q in exchange %q: %w", uid, pid, ename, err)
		}

		s.jobMap.Store(uid, v)
		defer s.jobMap.Delete(uid)

		return v.Run(ctx, s.Runtime(product))
	}
}

// doPause pauses a running job. If the job is not running and is not final
// it's state is updated to manually-paused state.
func (s *Server) doPause(ctx context.Context, req *api.JobPauseRequest) (*api.JobPauseResponse, error) {
	var state job.State
	pause := func(ctx context.Context, rw kv.ReadWriter) error {
		nstate, err := s.runner.Pause(ctx, rw, req.UID)
		if err != nil {
			return fmt.Errorf("could not pause job %q: %w", req.UID, err)
		}
		state = nstate

		jd, err := s.runner.Get(ctx, rw, req.UID)
		if err != nil {
			return fmt.Errorf("could not get job %q data: %w", req.UID, err)
		}
		if err := s.runner.UpdateFlags(ctx, rw, req.UID, jd.Flags|ManualFlag); err != nil {
			log.Printf("job is paused, but could not mark job %q as manual (ignored): %v", req.UID, err)
		}
		return nil
	}
	if err := kv.WithReadWriter(ctx, s.db, pause); err != nil {
		return nil, fmt.Errorf("could not pause job %q: %w", req.UID, err)
	}

	resp := &api.JobPauseResponse{
		FinalState: string(state),
	}
	return resp, nil
}

// doResume resumes a non-final job.
func (s *Server) doResume(ctx context.Context, req *api.JobResumeRequest) (*api.JobResumeResponse, error) {
	var state job.State
	resume := func(ctx context.Context, rw kv.ReadWriter) error {
		jd, err := s.runner.Get(ctx, rw, req.UID)
		if err != nil {
			return err
		}

		if jd.Flags&ManualFlag != 0 {
			jd.Flags = jd.Flags ^ ManualFlag
			if err := s.runner.UpdateFlags(ctx, rw, req.UID, jd.Flags); err != nil {
				log.Printf("could not clear the manual flag on job %q (ignored): %v", req.UID, err)
				return err
			}
		}

		nstate, err := s.resume(ctx, rw, jd)
		if err != nil {
			return fmt.Errorf("could not resume job: %w", err)
		}

		state = nstate
		return nil
	}
	if err := kv.WithReadWriter(ctx, s.db, resume); err != nil {
		return nil, err
	}

	resp := &api.JobResumeResponse{
		FinalState: string(state),
	}
	return resp, nil
}

// doCancel cancels a non-final job. If job is running, it will be stopped.
func (s *Server) doCancel(ctx context.Context, req *api.JobCancelRequest) (*api.JobCancelResponse, error) {
	state, err := job.CancelDB(ctx, s.runner, s.db, req.UID)
	if err != nil {
		return nil, err
	}
	resp := &api.JobCancelResponse{
		FinalState: string(state),
	}
	return resp, nil
}

func (s *Server) doList(ctx context.Context, req *api.JobListRequest) (*api.JobListResponse, error) {
	snap, err := s.db.NewSnapshot(ctx)
	if err != nil {
		return nil, fmt.Errorf("could not create a db snapshot: %w", err)
	}
	defer snap.Discard(ctx)

	resp := new(api.JobListResponse)
	collect := func(ctx context.Context, r kv.Reader, jd *job.JobData) error {
		name, _, _, err := namer.Resolve(ctx, snap, jd.UID)
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("could not resolve job id %q: %w", jd.UID, err)
			}
		}
		item := &api.JobListResponseItem{
			UID:        jd.UID,
			Type:       jd.Typename,
			State:      string(jd.State),
			Name:       name,
			ManualFlag: (jd.Flags & ManualFlag) != 0,
		}
		resp.Jobs = append(resp.Jobs, item)
		return nil
	}
	if err := job.ScanDB(ctx, s.runner, s.db, collect); err != nil {
		return nil, fmt.Errorf("could not scan all jobs: %w", err)
	}
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
		jd, err := s.runner.Get(ctx, rw, req.UID)
		if err != nil {
			return fmt.Errorf("could not load job %q: %w", req.UID, err)
		}
		if err := namer.SetName(ctx, rw, req.JobName, jd.UID, jd.Typename); err != nil {
			return fmt.Errorf("could not assign name: %w", err)
		}
		return nil
	}
	if err := kv.WithReadWriter(ctx, s.db, setName); err != nil {
		return nil, err
	}

	return &api.SetJobNameResponse{}, nil
}

func (s *Server) doJobSetOption(ctx context.Context, req *api.JobSetOptionRequest) (*api.JobSetOptionResponse, error) {
	if err := req.Check(); err != nil {
		return nil, fmt.Errorf("invalid rename request: %w", err)
	}

	if _, err := uuid.Parse(req.UID); err != nil {
		return nil, fmt.Errorf("job uid must be an uuid: %w", err)
	}

	// Job can be running or in paused state.
	update := func(ctx context.Context, rw kv.ReadWriter) error {
		if v, ok := s.jobMap.Load(req.UID); ok {
			if err := v.SetOption(req.OptionKey, req.OptionValue); err != nil {
				return err
			}
			if err := v.Save(ctx, rw); err != nil {
				return fmt.Errorf("could not save option change (deferred): %w", err)
			}
			return nil
		}

		jd, err := s.runner.Get(ctx, rw, req.UID)
		if err != nil {
			return err
		}

		if job.IsDone(jd.State) {
			return fmt.Errorf("job %q is already completed (%q)", req.UID, jd.State)
		}

		job, err := Load(ctx, rw, req.UID, jd.Typename)
		if err != nil {
			return fmt.Errorf("could not load trader job %q: %w", req.UID, err)
		}

		if err := job.SetOption(req.OptionKey, req.OptionValue); err != nil {
			return fmt.Errorf("could not set job option: %w", err)
		}
		return nil
	}
	if err := kv.WithReadWriter(ctx, s.db, update); err != nil {
		return nil, err
	}
	return &api.JobSetOptionResponse{}, nil
}
