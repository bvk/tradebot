// Copyright (c) 2026 BVK Chaitanya

package etrade

import (
	"context"
	"errors"
	"os/exec"
	"testing"
	"time"
)

// countingDisplayRunner records how many times RunWithDisplay was invoked,
// simulating a login subprocess that is stuck (e.g. behind a manual
// challenge) and always fails.
type countingDisplayRunner struct {
	calls int
	err   error
}

func (r *countingDisplayRunner) RunWithDisplay(cmd *exec.Cmd) error {
	r.calls++
	return r.err
}

func TestReloginCooldown(t *testing.T) {
	runner := &countingDisplayRunner{err: errors.New("login stuck behind manual challenge")}
	c := &Client{opts: Options{DisplayRunner: runner}}
	ctx := context.Background()

	if err := c.relogin(ctx); err == nil {
		t.Fatal("relogin: want error, got nil")
	}
	if runner.calls != 1 {
		t.Fatalf("after first relogin: got %d subprocess calls, want 1", runner.calls)
	}

	// A second relogin shortly after a failed attempt must not spawn another
	// subprocess; it should return the cached error instead.
	if err := c.relogin(ctx); err == nil {
		t.Fatal("relogin: want cached error, got nil")
	}
	if runner.calls != 1 {
		t.Fatalf("relogin within cooldown: got %d subprocess calls, want 1", runner.calls)
	}

	// Once the cooldown has elapsed, relogin should try again.
	c.reloginLastAttempt = time.Now().Add(-reloginCooldown - time.Second)
	if err := c.relogin(ctx); err == nil {
		t.Fatal("relogin: want error, got nil")
	}
	if runner.calls != 2 {
		t.Fatalf("relogin after cooldown: got %d subprocess calls, want 2", runner.calls)
	}
}
