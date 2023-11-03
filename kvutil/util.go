// Copyright (c) 2023 BVK Chaitanya

package kvutil

import (
	"context"
	"encoding/gob"
	"io"
	"strings"

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

func GetString[T ~string](ctx context.Context, g kv.Getter, key string) (T, error) {
	value, err := g.Get(ctx, key)
	if err != nil {
		return "", err
	}
	var sb strings.Builder
	if _, err := io.Copy(&sb, value); err != nil {
		return "", err
	}
	return T(sb.String()), nil
}

func SetString[T ~string](ctx context.Context, s kv.Setter, key string, value T) error {
	return s.Set(ctx, key, strings.NewReader(string(value)))
}
