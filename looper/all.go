// Copyright (c) 2025 BVK Chaitanya

package looper

import (
	"context"
	"errors"
	"io"
	"path"
	"strings"

	"github.com/bvkgo/kv"
)

func LoadAll(ctx context.Context, r kv.Reader) ([]*Looper, error) {
	return LoadFunc(ctx, r, nil)
}

func LoadFunc(ctx context.Context, r kv.Reader, pickf func(string) bool) ([]*Looper, error) {
	const MinUUID = "00000000-0000-0000-0000-000000000000"
	const MaxUUID = "ffffffff-ffff-ffff-ffff-ffffffffffff"

	begin := path.Join(DefaultKeyspace, MinUUID)
	end := path.Join(DefaultKeyspace, MaxUUID)

	it, err := r.Ascend(ctx, begin, end)
	if err != nil {
		return nil, err
	}
	defer kv.Close(it)

	var loopers []*Looper
	for k, _, err := it.Fetch(ctx, false); err == nil; k, _, err = it.Fetch(ctx, true) {
		if pickf != nil {
			if !pickf(k) {
				continue
			}
		}

		uid := strings.TrimPrefix(k, DefaultKeyspace)
		v, err := Load(ctx, uid, r)
		if err != nil {
			return nil, err
		}
		loopers = append(loopers, v)
	}

	if _, _, err := it.Fetch(ctx, false); err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	return loopers, nil
}
