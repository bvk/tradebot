// Copyright (c) 2023 BVK Chaitanya

package trader

import (
	"context"
	"io"

	"github.com/bvkgo/kv"
)

func kvGet(ctx context.Context, db kv.Database, key string) (v io.Reader, err error) {
	kv.WithSnapshot(ctx, db, func(ctx context.Context, s kv.Snapshot) error {
		v, err = s.Get(ctx, key)
		return err
	})
	return
}
