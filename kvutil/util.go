// Copyright (c) 2023 BVK Chaitanya

package kvutil

import (
	"context"
	"encoding/gob"

	"github.com/bvkgo/kv"
)

func Get[T any](ctx context.Context, g kv.Getter, key string) (*T, error) {
	value, err := g.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	gv := new(T)
	if err := gob.NewDecoder(value).Decode(gv); err != nil {
		return nil, err
	}
	return gv, nil
}
