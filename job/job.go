// Copyright (c) 2025 BVK Chaitanya

// Package job implements an api to manage jobs. Jobs are activities that can
// be canceled, paused or resumed through the context.Context argument.
package job

import (
	"context"
	"errors"

	"github.com/bvk/tradebot/gobs"
)

type Func func(ctx context.Context) error

var errDone = errors.New("no error")
var errPause = errors.New("ErrPause")
var errCancel = errors.New("ErrCancel")

type Job struct {
	lifeCtx    context.Context
	lifeCancel context.CancelFunc

	controlCtx    context.Context
	controlCancel context.CancelCauseFunc
}

func Run(fn Func, fctx context.Context) *Job {
	lifeCtx, lifeCancel := context.WithCancel(context.Background())
	controlCtx, controlCancel := context.WithCancelCause(fctx)

	v := &Job{
		lifeCtx:       lifeCtx,
		lifeCancel:    lifeCancel,
		controlCtx:    controlCtx,
		controlCancel: controlCancel,
	}
	go func() {
		defer lifeCancel()

		if err := fn(controlCtx); err != nil {
			controlCancel(err)
			return
		}
		controlCancel(errDone)
	}()

	return v
}

func (v *Job) Wait(ctx context.Context) error {
	var doneCh <-chan struct{}
	if ctx != nil {
		doneCh = ctx.Done()
	}
	select {
	case <-doneCh:
		return context.Cause(ctx)
	case <-v.lifeCtx.Done():
		return nil
	}
}

func (v *Job) Err() error {
	status := context.Cause(v.controlCtx)
	if status == nil || errors.Is(status, errDone) {
		return nil
	}
	return status
}

func (v *Job) state() gobs.State {
	status := context.Cause(v.controlCtx)
	switch {
	case status == nil:
		return gobs.RUNNING
	case errors.Is(status, errDone):
		return gobs.COMPLETED
	case errors.Is(status, errPause):
		return gobs.PAUSED
	case errors.Is(status, errCancel):
		return gobs.CANCELED
	default:
		return gobs.FAILED
	}
}

func (v *Job) Pause() {
	v.controlCancel(errPause)
}

func (v *Job) Cancel() {
	v.controlCancel(errCancel)
}
