// Copyright (c) 2023 BVK Chaitanya

package trader

import (
	"context"
	"encoding/gob"
	"io"

	"github.com/bvkgo/kv"
)

func kvGet[T any](ctx context.Context, db kv.Database, key string) (*T, error) {
	var value io.Reader
	reader := func(ctx context.Context, s kv.Snapshot) error {
		v, err := s.Get(ctx, key)
		if err != nil {
			return err
		}
		value = v
		return nil
	}
	if err := kv.WithSnapshot(ctx, db, reader); err != nil {
		return nil, err
	}
	gv := new(T)
	if err := gob.NewDecoder(value).Decode(gv); err != nil {
		return nil, err
	}
	return gv, nil
}
