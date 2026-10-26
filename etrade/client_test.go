// Copyright (c) 2026 Deepak Vankadaru

package etrade

import (
	"context"
	"errors"
	"os/exec"
	"testing"
	"time"
)

// countingDisplayRunner simulates an unattended login subprocess that always
// fails (e.g. stuck behind a manual challenge) and counts how many times it
// was invoked.
type countingDisplayRunner struct {
	calls int
}

func (c *countingDisplayRunner) RunWithDisplay(cmd *exec.Cmd) error {
	c.calls++
	return errors.New("fake display runner failure")
}

func TestReloginCooldown(t *testing.T) {
	runner := &countingDisplayRunner{}
	c := &Client{opts: Options{DisplayRunner: runner}}
	ctx := context.Background()

	if err := c.relogin(ctx); err == nil {
		t.Fatal("relogin: expected an error from the fake display runner")
	}
	if err := c.relogin(ctx); err == nil {
		t.Fatal("relogin: expected an error from the fake display runner")
	}
	if runner.calls != 1 {
		t.Fatalf("relogin: unattended login subprocess ran %d times within the cooldown window, want 1", runner.calls)
	}

	c.lastReloginAt = time.Now().Add(-2 * reloginCooldown)
	if err := c.relogin(ctx); err == nil {
		t.Fatal("relogin: expected an error from the fake display runner")
	}
	if runner.calls != 2 {
		t.Fatalf("relogin: unattended login subprocess did not retry after the cooldown window elapsed, got %d calls", runner.calls)
	}
}
