// Copyright (c) 2023 BVK Chaitanya

package job

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestPauseResume(t *testing.T) {
	jobf := func(ctx context.Context) error {
		t.Logf("resumed")
		<-ctx.Done()
		return context.Cause(ctx)
	}
	j1 := New("", jobf)
	if err := j1.Resume(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := j1.Pause(); err != nil {
		t.Fatal(err)
	}
	if err := j1.Resume(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := j1.Cancel(); err != nil {
		t.Fatal(err)
	}
	if err := j1.Resume(context.Background()); err == nil {
		t.Fatalf("Resume: wanted non-nil, got nil")
	}
	if err := j1.Pause(); err == nil {
		t.Fatalf("Pause: wanted non-nil, got nil")
	}
}

func TestCancel(t *testing.T) {
	jobf := func(ctx context.Context) error {
		t.Logf("resumed")
		<-ctx.Done()
		return context.Cause(ctx)
	}

	j1 := New("", jobf)
	if err := j1.Cancel(); err != nil {
		t.Fatal(err)
	}

	j2 := New("", jobf)
	if err := j2.Resume(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := j2.Cancel(); err != nil {
		t.Fatal(err)
	}

	j3 := New("", jobf)
	if err := j3.Pause(); err != nil {
		t.Fatal(err)
	}
	if err := j3.Cancel(); err != nil {
		t.Fatal(err)
	}
}

func TestFailed(t *testing.T) {
	jobf := func(ctx context.Context) error {
		return fmt.Errorf("error message")
	}

	j1 := New("", jobf)
	if err := j1.Resume(context.Background()); err != nil {
		t.Fatal(err)
	}
	for !IsFinal(j1.State()) {
		time.Sleep(time.Millisecond)
	}
	if !IsFailed(j1.State()) {
		t.Fatalf("IsFailed: wanted true, got false")
	}
}
