// Copyright (c) 2023 BVK Chaitanya

package job

import (
	"context"
	"errors"
	"os"
	"testing"
)

func TestPauseResume(t *testing.T) {
	jobf := func(ctx context.Context) error {
		t.Logf("resumed")
		<-ctx.Done()
		return context.Cause(ctx)
	}
	j1 := Run(jobf, context.Background())
	if j1.State() != RUNNING {
		t.Fatalf("j1 must be running")
	}
	j1.Pause()
	j1.Wait()
	if j1.State() != PAUSED {
		t.Fatalf("j1 must be paused")
	}
	if err := j1.Resume(context.Background()); err != nil {
		t.Fatal(err)
	}
	if j1.State() != RUNNING {
		t.Fatalf("j1 must be running again")
	}
	j1.Cancel()
	j1.Wait()
	if j1.State() != CANCELED {
		t.Fatalf("j1 must be canceled")
	}
	if err := j1.Resume(context.Background()); err == nil {
		t.Fatalf("j1 resume must've failed")
	} else if !errors.Is(err, os.ErrClosed) {
		t.Fatal(err)
	}
}
