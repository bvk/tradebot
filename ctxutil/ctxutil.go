// Copyright (c) 2023 BVK Chaitanya

package ctxutil

import (
	"context"
	"time"
)

// Sleep blocks the caller for given timeout duration. Returns early if the
// input context is canceled.
func Sleep(ctx context.Context, d time.Duration) {
	sctx, scancel := context.WithTimeout(ctx, d)
	<-sctx.Done()
	scancel()
}

// Retry runs the input function till it succeeds or till the input context is
// canceled. Returns nil if the input function is successful or last non-nil
// error from the function after the context has expired.
func Retry(ctx context.Context, interval time.Duration, f func() error) (err error) {
	for err = f(); err != nil && context.Cause(ctx) == nil; err = f() {
		Sleep(ctx, interval)
	}
	return
}

// Retry runs the input function till it succeeds or till the input context is canceled or the input timeout is
func RetryTimeout(ctx context.Context, interval, timeout time.Duration, f func() error) error {
	sctx, scancel := context.WithTimeout(ctx, timeout)
	defer scancel()
	return Retry(sctx, interval, f)
}
