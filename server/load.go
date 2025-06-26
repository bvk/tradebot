// Copyright (c) 2023 BVK Chaitanya

package server

import (
	"context"
	"fmt"
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

func LoadTraders(ctx context.Context, r kv.Reader) ([]trader.Trader, error) {
	var traders []trader.Trader

	limiterPick := func(k string) bool {
		_, err := uuid.Parse(strings.TrimPrefix(k, limiter.DefaultKeyspace))
		return err == nil
	}
	limiters, err := limiter.LoadFunc(ctx, r, limiterPick)
	if err != nil {
		return nil, fmt.Errorf("could not load all existing limiters: %w", err)
	}
	for _, v := range limiters {
		traders = append(traders, v)
	}

	looperPick := func(k string) bool {
		_, err := uuid.Parse(strings.TrimPrefix(k, looper.DefaultKeyspace))
		return err == nil
	}
	loopers, err := looper.LoadFunc(ctx, r, looperPick)
	if err != nil {
		return nil, fmt.Errorf("could not load all existing loopers: %w", err)
	}
	for _, v := range loopers {
		traders = append(traders, v)
	}

	wallerPick := func(k string) bool {
		_, err := uuid.Parse(strings.TrimPrefix(k, waller.DefaultKeyspace))
		return err == nil
	}
	wallers, err := waller.LoadFunc(ctx, r, wallerPick)
	if err != nil {
		return nil, fmt.Errorf("could not load all existing wallers: %w", err)
	}
	for _, v := range wallers {
		traders = append(traders, v)
	}

	return traders, nil
}

func Load(ctx context.Context, r kv.Reader, uid, typename string) (trader.Trader, error) {
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

func loadFromDB(ctx context.Context, db kv.Database, uid, typename string) (job trader.Trader, err error) {
	kv.WithReader(ctx, db, func(ctx context.Context, r kv.Reader) error {
		job, err = Load(ctx, r, uid, typename)
		return err
	})
	return
}
