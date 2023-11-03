// Copyright (c) 2023 BVK Chaitanya

// Package job implements an api to manage jobs. Jobs are activities that can
// be canceled, paused or resumed through the context.Context argument.
package job

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
)

type State string

type Func func(ctx context.Context) error

var (
	errPause  = errors.New("ErrPause")
	errCancel = errors.New("ErrCancel")
)

type Job struct {
	cancel context.CancelCauseFunc

	mu sync.Mutex
	wg sync.WaitGroup

	f Func

	status State
}

func New(last State, f Func) *Job {
	j := &Job{
		f:      f,
		status: last,
	}
	return j
}

func (j *Job) Close() {
	if j.cancel != nil {
		j.cancel(os.ErrClosed)
	}
	j.wg.Wait()
}

func (j *Job) Resume(ctx context.Context) error {
	j.mu.Lock()
	defer j.mu.Unlock()

	if IsFinal(j.status) {
		return os.ErrClosed
	}
	if j.status == "RESUMED" {
		return nil
	}

	jctx, jcancel := context.WithCancelCause(ctx)
	j.cancel, j.status = jcancel, "RESUMED"

	j.wg.Add(1)
	go j.goRun(jctx)
	return nil
}

func (j *Job) Cancel() error {
	defer j.wg.Wait()

	j.mu.Lock()
	defer j.mu.Unlock()

	if IsFinal(j.status) {
		return os.ErrClosed
	}

	if j.cancel != nil {
		j.cancel(errCancel)
		j.cancel = nil
	}
	j.status = "CANCELED"
	return nil
}

func (j *Job) Pause() error {
	defer j.wg.Wait()

	j.mu.Lock()
	defer j.mu.Unlock()

	if IsFinal(j.status) {
		return os.ErrClosed
	}

	if j.cancel != nil {
		j.cancel(errPause)
		j.cancel = nil
	}
	j.status = "PAUSED"
	return nil
}

func (j *Job) State() State {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.status
}

func (j *Job) setState(s State) {
	j.mu.Lock()
	defer j.mu.Unlock()

	if IsFinal(j.status) {
		panic("job was already finalized")
	}
	j.status = s
}

func (j *Job) goRun(ctx context.Context) {
	defer j.wg.Done()

	err := j.f(ctx)

	switch {
	case err == nil:
		j.setState("COMPLETED")
	case !errors.Is(err, context.Cause(ctx)):
		j.setState(State(fmt.Sprintf("FAILED: %s", err.Error())))
	}
}

func IsFinal(s State) bool {
	return s == "COMPLETED" || s == "CANCELED" || strings.HasPrefix(string(s), "FAILED:")
}

func IsFailed(s State) bool {
	return strings.HasPrefix(string(s), "FAILED:")
}
