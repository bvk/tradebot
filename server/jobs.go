// Copyright (c) 2023 BVK Chaitanya

package server

import (
	"context"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"os"
	"time"

	"github.com/bvk/tradebot/api"
	"github.com/bvk/tradebot/exchange"
	"github.com/bvk/tradebot/gobs"
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

		var err error
		var product exchange.Product
		for i := range 10 /* retry 10 times */ {
			product, err = s.getProduct(ctx, ename, pid)
			if err != nil {
				slog.Error("could not load product for the job (will retry)", "retry", i, "job", uid, "product", pid, "exchange", ename, "err", err)
				time.Sleep(time.Second)
				continue
			}
			break
		}
		if product == nil {
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
	if err := s.runner.Pause(ctx, req.UID); err != nil {
		return nil, fmt.Errorf("could not pause job %q: %w", req.UID, err)
	}

	var state gobs.State
	pause := func(ctx context.Context, rw kv.ReadWriter) error {
		jd, err := s.runner.Get(ctx, rw, req.UID)
		if err != nil {
			return fmt.Errorf("could not get job %q data: %w", req.UID, err)
		}
		if err := s.runner.UpdateFlags(ctx, rw, req.UID, jd.Flags|ManualFlag); err != nil {
			log.Printf("job is paused, but could not mark job %q as manual (ignored): %v", req.UID, err)
		}
		state = jd.State
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
	var trader trader.Trader
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

		v, err := Load(ctx, rw, req.UID, jd.Typename)
		if err != nil {
			return fmt.Errorf("could not load trader job %q: %w", req.UID, err)
		}
		trader = v
		return nil
	}
	if err := kv.WithReadWriter(ctx, s.db, resume); err != nil {
		return nil, err
	}

	if err := s.runner.Resume(ctx, req.UID, s.makeJobFunc(trader), s.cg.Context()); err != nil {
		return nil, fmt.Errorf("could not resume job %q: %w", req.UID, err)
	}
	log.Printf("resumed job with id %q", req.UID)

	resp := &api.JobResumeResponse{
		FinalState: string(gobs.RUNNING),
	}
	return resp, nil
}

// doCancel cancels a non-final job. If job is running, it will be stopped.
func (s *Server) doCancel(ctx context.Context, req *api.JobCancelRequest) (*api.JobCancelResponse, error) {
	jd, err := s.runner.Cancel(ctx, req.UID)
	if err != nil {
		return nil, err
	}
	resp := &api.JobCancelResponse{
		FinalState: string(jd.State),
	}
	return resp, nil
}

func (s *Server) doList(ctx context.Context, req *api.JobListRequest) (*api.JobListResponse, error) {
	resp := new(api.JobListResponse)
	collect := func(ctx context.Context, r kv.Reader, jd *gobs.JobData) error {
		name, _, _, err := namer.Resolve(ctx, r, jd.ID)
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("could not resolve job id %q: %w", jd.ID, err)
			}
		}
		item := &api.JobListResponseItem{
			UID:        jd.ID,
			Type:       jd.Typename,
			State:      string(jd.State),
			Name:       name,
			ManualFlag: (jd.Flags & ManualFlag) != 0,
		}
		resp.Jobs = append(resp.Jobs, item)
		return nil
	}
	if err := s.runner.Scan(ctx, nil /* kv.Reader */, collect); err != nil {
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
		if err := namer.SetName(ctx, rw, req.JobName, jd.ID, jd.Typename); err != nil {
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

	update := func(ctx context.Context, rw kv.ReadWriter) error {
		// Job must not be running.
		if _, ok := s.jobMap.Load(req.UID); ok {
			return fmt.Errorf("job is currently running: %w", os.ErrInvalid)
		}

		// Job must not be complete already.
		jd, err := s.runner.Get(ctx, rw, req.UID)
		if err != nil {
			return err
		}

		if jd.State.IsDone() {
			return fmt.Errorf("job %q is already completed (%q)", req.UID, jd.State)
		}

		job, err := Load(ctx, rw, req.UID, jd.Typename)
		if err != nil {
			return fmt.Errorf("could not load trader job %q: %w", req.UID, err)
		}
		if _, err := job.SetOption(req.OptionKey, req.OptionValue); err != nil {
			return fmt.Errorf("could not set job option: %w", err)
		}
		if err := job.Save(ctx, rw); err != nil {
			return fmt.Errorf("could not save the job options: %w", err)
		}
		return nil
	}
	if err := kv.WithReadWriter(ctx, s.db, update); err != nil {
		return nil, err
	}
	return &api.JobSetOptionResponse{}, nil
}
