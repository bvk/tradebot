// Copyright (c) 2023 BVK Chaitanya

package waller

import (
	"context"
	"log"
	"log/slog"
	"runtime/debug"
	"sync"
	"time"

	"github.com/bvk/tradebot/looper"
	"github.com/bvk/tradebot/trader"
	"github.com/bvkgo/kv"
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

	if w.summary.Load() == nil {
		if err := kv.WithReadWriter(ctx, rt.Database, w.Save); err != nil {
			return err
		}
	}

	jobUpdatesCh := make(chan string, len(w.loopers))
	ctx = trader.WithJobUpdateChannel(ctx, jobUpdatesCh)

	loopMap := make(map[string]*looper.Looper)
	for _, loop := range w.loopers {
		loop := loop
		loopMap[loop.UID()] = loop

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
					continue
				}
				// Looper job is completed successfully.
				return
			}
		}()
	}

	for ctx.Err() != nil {
		select {
		case uid := <-jobUpdatesCh:
			if _, ok := loopMap[uid]; ok {
				if err := kv.WithReadWriter(ctx, rt.Database, w.Save); err != nil {
					slog.Error("could not save waller to the database (ignored; will retry)", "err", err)
				}
			}
		}
	}

	wg.Wait()
	return context.Cause(ctx)
}
