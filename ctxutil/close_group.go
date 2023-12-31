// Copyright (c) 2023 BVK Chaitanya

package ctxutil

import (
	"context"
	"os"
	"sync"
	"time"
)

type CloseGroup struct {
	closeCtx  context.Context
	causeFunc context.CancelCauseFunc

	wg sync.WaitGroup

	once sync.Once
}

func (cg *CloseGroup) init() {
	cg.closeCtx, cg.causeFunc = context.WithCancelCause(context.Background())
}

func (cg *CloseGroup) Close() {
	cg.once.Do(cg.init)
	cg.causeFunc(os.ErrClosed)
	cg.wg.Wait()
}

func (cg *CloseGroup) Context() context.Context {
	cg.once.Do(cg.init)
	return cg.closeCtx
}

func (cg *CloseGroup) Go(f func(ctx context.Context)) {
	cg.once.Do(cg.init)

	cg.wg.Add(1)
	go func() {
		f(cg.closeCtx)
		cg.wg.Done()
	}()
}

func (cg *CloseGroup) AfterDurationFunc(d time.Duration, f func(context.Context)) {
	cg.wg.Add(1)
	go func() {
		Sleep(cg.closeCtx, d)
		if cg.closeCtx.Err() == nil {
			f(cg.closeCtx)
		}
		cg.wg.Done()
	}()
}
