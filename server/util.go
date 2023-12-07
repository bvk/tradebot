// Copyright (c) 2023 BVK Chaitanya

package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"

	"github.com/bvk/tradebot/limiter"
	"github.com/bvk/tradebot/looper"
	"github.com/bvk/tradebot/trader"
	"github.com/bvk/tradebot/waller"
	"github.com/bvkgo/kv"
	"github.com/google/uuid"
)

func load[T trader.Job](ctx context.Context, r kv.Reader, keyspace string, loader func(context.Context, string, kv.Reader) (T, error)) ([]trader.Job, error) {
	begin := path.Join(keyspace, minUUID)
	end := path.Join(keyspace, maxUUID)

	it, err := r.Ascend(ctx, begin, end)
	if err != nil {
		return nil, err
	}
	defer kv.Close(it)

	var jobs []trader.Job
	for k, _, err := it.Fetch(ctx, false); err == nil; k, _, err = it.Fetch(ctx, true) {
		uid := strings.TrimPrefix(k, keyspace)
		if _, err := uuid.Parse(uid); err != nil {
			continue
		}

		v, err := loader(ctx, uid, r)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, v)
	}

	if _, _, err := it.Fetch(ctx, false); err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	return jobs, nil
}

func LoadTraders(ctx context.Context, r kv.Reader) ([]trader.Job, error) {
	var traders []trader.Job

	limiters, err := load(ctx, r, limiter.DefaultKeyspace, limiter.Load)
	if err != nil {
		return nil, fmt.Errorf("could not load all existing limiters: %w", err)
	}
	traders = append(traders, limiters...)

	loopers, err := load(ctx, r, looper.DefaultKeyspace, looper.Load)
	if err != nil {
		return nil, fmt.Errorf("could not load all existing loopers: %w", err)
	}
	traders = append(traders, loopers...)

	wallers, err := load(ctx, r, waller.DefaultKeyspace, waller.Load)
	if err != nil {
		return nil, fmt.Errorf("could not load all existing wallers: %w", err)
	}
	traders = append(traders, wallers...)

	return traders, nil
}

func Load(ctx context.Context, r kv.Reader, uid string) (trader.Job, error) {
	if v, err := limiter.Load(ctx, uid, r); err == nil {
		return v, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("could not load limiter: %w", err)
	}

	if v, err := looper.Load(ctx, uid, r); err == nil {
		return v, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("could not load looper: %w", err)
	}

	if v, err := waller.Load(ctx, uid, r); err == nil {
		return v, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("could not load waller: %w", err)
	}
	return nil, os.ErrNotExist
}

func loadFromDB(ctx context.Context, db kv.Database, uid string) (job trader.Job, err error) {
	kv.WithReader(ctx, db, func(ctx context.Context, r kv.Reader) error {
		job, err = Load(ctx, r, uid)
		return err
	})
	return
}
