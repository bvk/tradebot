// Copyright (c) 2023 BVK Chaitanya

package kvutil

import (
	"bytes"
	"context"
	"encoding/gob"
	"fmt"
	"io"
	"strings"

	"github.com/bvkgo/kv"
)

func Get[T any](ctx context.Context, g kv.Getter, key string) (*T, error) {
	value, err := g.Get(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("could not Get from %q: %w", key, err)
	}
	gv := new(T)
	if err := gob.NewDecoder(value).Decode(gv); err != nil {
		return nil, fmt.Errorf("could not gob-decode value at key %q: %w", key, err)
	}
	return gv, nil
}

func Set[T any](ctx context.Context, s kv.Setter, key string, value *T) error {
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(value); err != nil {
		return err
	}
	return s.Set(ctx, key, &buf)
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

func GetDB[T any](ctx context.Context, db kv.Database, key string) (value *T, err error) {
	err = kv.WithReader(ctx, db, func(ctx context.Context, r kv.Reader) error {
		value, err = Get[T](ctx, r, key)
		return err
	})
	return value, err
}

func SetDB[T any](ctx context.Context, db kv.Database, key string, value *T) error {
	return kv.WithReadWriter(ctx, db, func(ctx context.Context, rw kv.ReadWriter) error {
		return Set[T](ctx, rw, key, value)
	})
}
