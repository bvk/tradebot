// Copyright (c) 2023 BVK Chaitanya

package waller

import (
	"context"
	"log"
	"log/slog"
	"runtime/debug"
	"sync"
	"time"

	"github.com/bvk/tradebot/trader"
)

func (w *Waller) Fix(ctx context.Context, rt *trader.Runtime) error {
	for _, l := range w.loopers {
		if err := l.Fix(ctx, rt); err != nil {
			return err
		}
	}
	return nil
}

func (w *Waller) Refresh(ctx context.Context, rt *trader.Runtime) error {
	for _, l := range w.loopers {
		if err := l.Refresh(ctx, rt); err != nil {
			return err
		}
	}
	return nil
}

func (w *Waller) Run(ctx context.Context, rt *trader.Runtime) error {
	log.Printf("started waller %s", w.uid)
	var wg sync.WaitGroup

	for _, loop := range w.loopers {
		loop := loop

		wg.Add(1)
		go func() {
			defer wg.Done()

			defer func() {
				if r := recover(); r != nil {
					slog.Error("CAUGHT PANIC", "panic", r)
					slog.Error(string(debug.Stack()))
					panic(r)
				}
			}()

			for ctx.Err() == nil {
				if err := loop.Run(ctx, rt); err != nil {
					if ctx.Err() == nil {
						log.Printf("wall-looper %v has failed (retry): %v", loop, err)
						time.Sleep(time.Second)
					}
				}
			}
		}()
	}

	wg.Wait()
	return context.Cause(ctx)
}
