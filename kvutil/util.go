// Copyright (c) 2023 BVK Chaitanya

package kvutil

import (
	"bytes"
	"context"
	"encoding/gob"
	"errors"
	"fmt"
	"io"
	"path"
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

type IterFunc[T any] func(context.Context, kv.Reader, string, *T) error

func Ascend[T any](ctx context.Context, r kv.Reader, begin, end string, fn IterFunc[T]) error {
	it, err := r.Ascend(ctx, begin, end)
	if err != nil {
		return err
	}
	defer kv.Close(it)

	for k, v, err := it.Fetch(ctx, false); err == nil; k, v, err = it.Fetch(ctx, true) {
		gv := new(T)
		if err := gob.NewDecoder(v).Decode(gv); err != nil {
			return fmt.Errorf("could not decode value at key %q: %w", k, err)
		}
		if err := fn(ctx, r, k, gv); err != nil {
			return err
		}
	}

	if _, _, err := it.Fetch(ctx, false); err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("could not complete ascend: %w", err)
	}
	return nil
}

func AscendDB[T any](ctx context.Context, db kv.Database, begin, end string, fn IterFunc[T]) error {
	return kv.WithReader(ctx, db, func(ctx context.Context, r kv.Reader) error {
		return Ascend[T](ctx, r, begin, end, fn)
	})
}

func PathRange(dir string) (begin string, end string) {
	dir = path.Clean(dir)
	if dir == "/" {
		return "", ""
	}
	begin = dir + string('/')
	end = dir + string('/'+1)
	return begin, end
}

// First returns the first key and value in the given range. Returns empty
// string and nil if the range is empty. Returns a non-empty key and nil value
// with a non-nil error if value could not be gob-decoded into given type.
func First[T any](ctx context.Context, r kv.Reader, begin, end string) (string, *T, error) {
	it, err := r.Ascend(ctx, begin, end)
	if err != nil {
		return "", nil, err
	}
	defer kv.Close(it)

	k, v, err := it.Fetch(ctx, false)
	if err != nil {
		if !errors.Is(err, io.EOF) {
			return "", nil, fmt.Errorf("could not complete ascend: %w", err)
		}
		return "", nil, nil
	}

	gv := new(T)
	if err := gob.NewDecoder(v).Decode(gv); err != nil {
		return k, nil, fmt.Errorf("could not decode value at key %q: %w", k, err)
	}
	return k, gv, nil
}

func FirstDB[T any](ctx context.Context, db kv.Database, begin, end string) (key string, value *T, err error) {
	kv.WithReader(ctx, db, func(ctx context.Context, r kv.Reader) error {
		key, value, err = First[T](ctx, r, begin, end)
		return nil
	})
	return key, value, err
}

// Last is similar to First, but returns the last key and value from the given range.
func Last[T any](ctx context.Context, r kv.Reader, begin, end string) (string, *T, error) {
	it, err := r.Descend(ctx, begin, end)
	if err != nil {
		return "", nil, err
	}
	defer kv.Close(it)

	k, v, err := it.Fetch(ctx, false)
	if err != nil {
		if !errors.Is(err, io.EOF) {
			return "", nil, fmt.Errorf("could not complete ascend: %w", err)
		}
		return "", nil, nil
	}

	gv := new(T)
	if err := gob.NewDecoder(v).Decode(gv); err != nil {
		return k, nil, fmt.Errorf("could not decode value at key %q: %w", k, err)
	}
	return k, gv, nil
}

func LastDB[T any](ctx context.Context, db kv.Database, begin, end string) (key string, value *T, err error) {
	kv.WithReader(ctx, db, func(ctx context.Context, r kv.Reader) error {
		key, value, err = Last[T](ctx, r, begin, end)
		return nil
	})
	return key, value, err
}
