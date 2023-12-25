// Copyright (c) 2023 BVK Chaitanya

package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"

	"github.com/bvk/tradebot/limiter"
	"github.com/bvk/tradebot/looper"
	"github.com/bvk/tradebot/namer"
	"github.com/bvk/tradebot/trader"
	"github.com/bvk/tradebot/waller"
	"github.com/bvkgo/kv"
	"github.com/google/uuid"
)

func load[T trader.Job](ctx context.Context, r kv.Reader, keyspace string, loader func(context.Context, string, kv.Reader) (T, error)) ([]trader.Job, error) {
	begin := path.Join(keyspace, MinUUID)
	end := path.Join(keyspace, MaxUUID)

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

func Load(ctx context.Context, r kv.Reader, uid, typename string) (trader.Job, error) {
	if typename == "" {
		if _, _, typ, err := namer.Resolve(ctx, r, uid); err == nil {
			typename = typ
		}
	}

	if typename == "" {
		kss := [][2]string{
			{limiter.DefaultKeyspace, "limiter"},
			{looper.DefaultKeyspace, "looper"},
			{waller.DefaultKeyspace, "waller"},
		}
		for _, ks := range kss {
			key := path.Join(ks[0], uid)
			if _, err := r.Get(ctx, key); err == nil {
				typename = ks[1]
				break
			}
		}
	}

	switch {
	case strings.EqualFold(typename, "limiter"):
		return limiter.Load(ctx, uid, r)
	case strings.EqualFold(typename, "looper"):
		return looper.Load(ctx, uid, r)
	case strings.EqualFold(typename, "waller"):
		return waller.Load(ctx, uid, r)
	}

	return nil, fmt.Errorf("unsupported trader type %q", typename)
}

func loadFromDB(ctx context.Context, db kv.Database, uid, typename string) (job trader.Job, err error) {
	kv.WithReader(ctx, db, func(ctx context.Context, r kv.Reader) error {
		job, err = Load(ctx, r, uid, typename)
		return err
	})
	return
}
