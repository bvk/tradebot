// Copyright (c) 2023 BVK Chaitanya

package trader

import (
	"context"
	"log/slog"
	"sync"

	"github.com/bvkgo/kv"
)

type Waller struct {
	loops []*Looper
}

func (v *Waller) check() error {
	return nil
}

func (v *Waller) Run(ctx context.Context, db kv.Database) error {
	var wg sync.WaitGroup
	defer wg.Wait()

	for _, loop := range v.loops {
		loop := loop

		wg.Add(1)
		go func() {
			defer wg.Done()

			if err := loop.Run(ctx, db); err != nil {
				slog.ErrorContext(ctx, "sub looper for wall has failed", "error", err)
				return
			}
		}()
	}
	return nil
}
