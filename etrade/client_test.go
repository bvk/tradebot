// Copyright (c) 2026 Deepak Vankadaru

package etrade

import (
	"context"
	"strings"
	"testing"
)

// TestReloginCooldown verifies that a second relogin attempt made shortly
// after a failure doesn't re-run the login subprocess, but instead returns a
// cooldown error. Without this, every 401 from every background poller would
// re-attempt the unattended login subprocess with no limit.
func TestReloginCooldown(t *testing.T) {
	c := &Client{}
	ctx := context.Background()

	err1 := c.relogin(ctx)
	if err1 == nil {
		t.Fatalf("relogin: want error with nil DisplayRunner, got nil")
	}

	err2 := c.relogin(ctx)
	if err2 == nil {
		t.Fatalf("relogin: want cooldown error on second attempt, got nil")
	}
	if !strings.Contains(err2.Error(), "cooldown") {
		t.Fatalf("relogin: want cooldown error on second attempt, got %q", err2.Error())
	}
}
