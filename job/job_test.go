// Copyright (c) 2023 BVK Chaitanya

package job

import (
	"context"
	"errors"
	"testing"

	"github.com/bvk/tradebot/gobs"
)

func TestPause(t *testing.T) {
	ctx := context.Background()
	jobf := func(ctx context.Context) error {
		<-ctx.Done()
		return context.Cause(ctx)
	}
	j1 := Run(jobf, ctx)
	if j1.state() != gobs.RUNNING {
		t.Fatalf("j1 must be running")
	}
	j1.Pause()
	j1.Wait(ctx)
	if j1.state() != gobs.PAUSED {
		t.Fatalf("j1 must be paused")
	}
	if !errors.Is(j1.Err(), errPause) {
		t.Fatalf("want errPause, got %v", j1.Err())
	}
}

func TestCancel(t *testing.T) {
	ctx := context.Background()
	jobf := func(ctx context.Context) error {
		<-ctx.Done()
		return context.Cause(ctx)
	}
	j1 := Run(jobf, ctx)
	if j1.state() != gobs.RUNNING {
		t.Fatalf("j1 must be running")
	}
	j1.Cancel()
	j1.Wait(ctx)
	if j1.state() != gobs.CANCELED {
		t.Fatalf("j1 must be paused")
	}
	if !errors.Is(j1.Err(), errCancel) {
		t.Fatalf("want errCancel, got %v", j1.Err())
	}
}

func TestFailed(t *testing.T) {
	ctx := context.Background()
	ch := make(chan error)
	jobf := func(ctx context.Context) error {
		return <-ch
	}
	j1 := Run(jobf, ctx)
	if j1.state() != gobs.RUNNING {
		t.Fatalf("j1 must be running")
	}
	errFailure := errors.New("operation failed")
	go func() { ch <- errFailure; close(ch) }()
	j1.Wait(ctx)
	if j1.state() != gobs.FAILED {
		t.Fatalf("j1 must have failed")
	}
	if !errors.Is(j1.Err(), errFailure) {
		t.Fatalf("want errFailure, got %v", j1.Err())
	}
}

func TestComplete(t *testing.T) {
	ctx := context.Background()
	ch := make(chan struct{})
	jobf := func(ctx context.Context) error {
		<-ch
		return context.Cause(ctx)
	}
	j1 := Run(jobf, ctx)
	if j1.state() != gobs.RUNNING {
		t.Fatalf("j1 must be running")
	}
	go func() { close(ch) }()
	j1.Wait(ctx)
	if j1.state() != gobs.COMPLETED {
		t.Fatalf("j1 must be complete, got %v (%v)", j1.state(), j1.Err())
	}
	if err := j1.Err(); err != nil {
		t.Fatal(err)
	}
}
