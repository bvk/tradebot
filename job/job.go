// Copyright (c) 2023 BVK Chaitanya

// Package job implements an api to manage jobs. Jobs are activities that can
// be canceled, paused or resumed through the context.Context argument.
package job

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"runtime/debug"
	"sync"
)

type Func func(ctx context.Context) error

var errPause = errors.New("ErrPause")
var errCancel = errors.New("ErrCancel")

type Job struct {
	cancel context.CancelCauseFunc

	wg sync.WaitGroup

	f Func

	mu sync.Mutex

	err  error
	done bool
}

func Run(fn Func, ctx context.Context) *Job {
	jctx, jcancel := context.WithCancelCause(ctx)

	j := &Job{
		cancel: jcancel,
		f:      fn,
	}

	j.wg.Add(1)
	go j.goRun(jctx)
	return j
}

func (j *Job) Wait() {
	j.wg.Wait()
}

func (j *Job) State() State {
	j.mu.Lock()
	defer j.mu.Unlock()

	return j.stateLocked()
}

func (j *Job) stateLocked() State {
	if !j.done {
		return RUNNING
	}
	if j.err == nil {
		return COMPLETED
	}
	if errors.Is(j.err, errPause) {
		return PAUSED
	}
	if errors.Is(j.err, errCancel) {
		return CANCELED
	}
	return FAILED
}

func (j *Job) Err() error {
	j.mu.Lock()
	defer j.mu.Unlock()

	return j.err
}

func (j *Job) Resume(ctx context.Context) error {
	j.mu.Lock()
	defer j.mu.Unlock()

	state := j.stateLocked()
	if state != PAUSED {
		if state == RUNNING {
			return os.ErrInvalid
		}
		return os.ErrClosed
	}

	jctx, jcancel := context.WithCancelCause(ctx)

	j.done = false
	j.cancel = jcancel

	j.wg.Add(1)
	go j.goRun(jctx)
	return nil
}

func (j *Job) Pause() {
	if j.cancel != nil {
		j.cancel(errPause)
		j.cancel = nil
	}
}

func (j *Job) Cancel() {
	if j.cancel != nil {
		j.cancel(errCancel)
		j.cancel = nil
	}
}

func (j *Job) goRun(ctx context.Context) {
	defer j.wg.Done()

	defer func() {
		if r := recover(); r != nil {
			slog.Error("CAUGHT PANIC", "panic", r)
			slog.Error(string(debug.Stack()))
			panic(r)
		}
	}()

	err := j.f(ctx)

	j.mu.Lock()
	defer j.mu.Unlock()

	j.err = err
	j.done = true
}
