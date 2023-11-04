// Copyright (c) 2023 BVK Chaitanya

package dbutil

import (
	"context"

	"github.com/bvkgo/kv"
	"github.com/bvkgo/tradebot/kvutil"
)

func Get[T any](ctx context.Context, db kv.Database, key string) (value *T, err error) {
	kv.WithReader(ctx, db, func(ctx context.Context, r kv.Reader) error {
		value, err = kvutil.Get[T](ctx, r, key)
		return err
	})
	return
}

func Set[T any](ctx context.Context, db kv.Database, key string, value *T) error {
	return kv.WithReadWriter(ctx, db, func(ctx context.Context, rw kv.ReadWriter) error {
		return kvutil.Set(ctx, rw, key, value)
	})
}
