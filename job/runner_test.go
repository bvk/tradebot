// Copyright (c) 2025 BVK Chaitanya

package job

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/bvk/tradebot/gobs"
	"github.com/bvkgo/kv/kvmemdb"
)

func TestRunner1(t *testing.T) {
	ctx := context.Background()
	db := kvmemdb.New()

	runner := NewRunner(db)
	defer runner.PauseAll(ctx)

	if err := runner.Add(ctx, nil, "1", "JobOne"); err != nil {
		t.Fatal(err)
	}
	if err := runner.Add(ctx, nil, "1", "OtherJob"); err == nil || !errors.Is(err, os.ErrExist) {
		t.Fatalf("wanted ErrExist, got %v", err)
	}

	if jd, err := runner.Get(ctx, nil, "1"); err != nil {
		t.Fatal(err)
	} else if jd.State != gobs.PAUSED {
		t.Fatalf("wanted PAUSED, got %v", jd.State)
	}

	ch := make(chan error)
	jobFunc := func(ctx context.Context) error {
		select {
		case <-ctx.Done():
			return context.Cause(ctx)
		case err := <-ch:
			return err
		}
	}

	if err := runner.Resume(ctx, "1", jobFunc, ctx); err != nil {
		t.Fatal(err)
	}

	if jd, err := runner.Get(ctx, nil, "1"); err != nil {
		t.Fatal(err)
	} else if jd.State != gobs.RUNNING {
		t.Fatalf("wanted RUNNING, got %v", jd.State)
	}

	if err := runner.Pause(ctx, "1"); err != nil {
		t.Fatal(err)
	}
	if jd, err := runner.Get(ctx, nil, "1"); err != nil {
		t.Fatal(err)
	} else if jd.State != gobs.PAUSED {
		t.Fatalf("wanted PAUSED, got %v", jd.State)
	}

	if err := runner.Resume(ctx, "1", jobFunc, ctx); err != nil {
		t.Fatal(err)
	}

	// Cancel a running job.
	if _, err := runner.Cancel(ctx, "1"); err != nil {
		t.Fatal(err)
	}
	if jd, err := runner.Get(ctx, nil, "1"); err != nil {
		t.Fatal(err)
	} else if jd.State != gobs.CANCELED {
		t.Fatalf("wanted CANCELED, got %v", jd.State)
	}

	if err := runner.Resume(ctx, "1", jobFunc, ctx); err == nil {
		t.Fatalf("wanted non-nil, got %v", err)
	}
}

func TestRunner2(t *testing.T) {
	ctx := context.Background()
	db := kvmemdb.New()

	runner := NewRunner(db)
	defer runner.PauseAll(ctx)

	if err := runner.Add(ctx, nil, "1", "JobOne"); err != nil {
		t.Fatal(err)
	}
	if err := runner.Add(ctx, nil, "1", "OtherJob"); err == nil || !errors.Is(err, os.ErrExist) {
		t.Fatalf("wanted ErrExist, got %v", err)
	}

	if jd, err := runner.Get(ctx, nil, "1"); err != nil {
		t.Fatal(err)
	} else if jd.State != gobs.PAUSED {
		t.Fatalf("wanted PAUSED, got %v", jd.State)
	}

	ch := make(chan error)
	jobFunc := func(ctx context.Context) error {
		select {
		case <-ctx.Done():
			return context.Cause(ctx)
		case err := <-ch:
			return err
		}
	}

	if err := runner.Resume(ctx, "1", jobFunc, ctx); err != nil {
		t.Fatal(err)
	}

	if jd, err := runner.Get(ctx, nil, "1"); err != nil {
		t.Fatal(err)
	} else if jd.State != gobs.RUNNING {
		t.Fatalf("wanted RUNNING, got %v", jd.State)
	}

	if err := runner.Pause(ctx, "1"); err != nil {
		t.Fatal(err)
	}
	if jd, err := runner.Get(ctx, nil, "1"); err != nil {
		t.Fatal(err)
	} else if jd.State != gobs.PAUSED {
		t.Fatalf("wanted PAUSED, got %v", jd.State)
	}

	// Cancel a PAUSED job.
	if _, err := runner.Cancel(ctx, "1"); err != nil {
		t.Fatal(err)
	}
	if jd, err := runner.Get(ctx, nil, "1"); err != nil {
		t.Fatal(err)
	} else if jd.State != gobs.CANCELED {
		t.Fatalf("wanted CANCELED, got %v", jd.State)
	}

	if err := runner.Resume(ctx, "1", jobFunc, ctx); err == nil {
		t.Fatalf("wanted non-nil, got %v", err)
	}
}

func TestRunner3(t *testing.T) {
	ctx := context.Background()
	db := kvmemdb.New()

	runner := NewRunner(db)
	defer runner.PauseAll(ctx)

	{
		tx, err := db.NewTransaction(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(ctx)

		if err := runner.Add(ctx, tx, "1", "JobOne"); err != nil {
			t.Fatal(err)
		}
		if err := runner.Add(ctx, tx, "1", "OtherJob"); err == nil || !errors.Is(err, os.ErrExist) {
			t.Fatalf("wanted ErrExist, got %v", err)
		}

		if jd, err := runner.Get(ctx, tx, "1"); err != nil {
			t.Fatal(err)
		} else if jd.State != gobs.PAUSED {
			t.Fatalf("wanted PAUSED, got %v", jd.State)
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
	}

	ch := make(chan error)
	jobFunc := func(ctx context.Context) error {
		select {
		case <-ctx.Done():
			return context.Cause(ctx)
		case err := <-ch:
			return err
		}
	}
	if err := runner.Resume(ctx, "1", jobFunc, ctx); err != nil {
		t.Fatal(err)
	}

	if jd, err := runner.Get(ctx, nil, "1"); err != nil {
		t.Fatal(err)
	} else if jd.State != gobs.RUNNING {
		t.Fatalf("wanted RUNNING, got %v", jd.State)
	}

	if err := runner.Pause(ctx, "1"); err != nil {
		t.Fatal(err)
	}
	if jd, err := runner.Get(ctx, nil, "1"); err != nil {
		t.Fatal(err)
	} else if jd.State != gobs.PAUSED {
		t.Fatalf("wanted PAUSED, got %v", jd.State)
	}

	if _, err := runner.Cancel(ctx, "1"); err != nil {
		t.Fatal(err)
	}
	if jd, err := runner.Get(ctx, nil, "1"); err != nil {
		t.Fatal(err)
	} else if jd.State != gobs.CANCELED {
		t.Fatalf("wanted CANCELED, got %v", jd.State)
	}

	if err := runner.Resume(ctx, "1", jobFunc, ctx); err == nil {
		t.Fatalf("wanted non-nil, got %v", err)
	}
}
