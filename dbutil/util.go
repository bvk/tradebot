// Copyright (c) 2023 BVK Chaitanya

package dbutil

import (
	"context"
	"strings"

	"github.com/bvkgo/kv"
	"github.com/bvkgo/tradebot/kvutil"
)

func SetString[T ~string](ctx context.Context, db kv.Database, key string, value T) error {
	return kv.WithReadWriter(ctx, db, func(ctx context.Context, rw kv.ReadWriter) error {
		return rw.Set(ctx, key, strings.NewReader(string(value)))
	})
}

func GetString[T ~string](ctx context.Context, db kv.Database, key string) (value T, err error) {
	kv.WithReader(ctx, db, func(ctx context.Context, r kv.Reader) error {
		value, err = kvutil.GetString[T](ctx, r, key)
		return err
	})
	return
}
