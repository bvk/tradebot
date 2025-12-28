// Copyright (c) 2025 BVK Chaitanya

package looper

import (
	"context"
	"fmt"
	"path"

	"github.com/bvk/tradebot/gobs"
	"github.com/bvk/tradebot/kvutil"
	"github.com/bvk/tradebot/timerange"
	"github.com/bvkgo/kv"
)

func Summary(ctx context.Context, r kv.Reader, uid string, period *timerange.Range, recal bool) (*gobs.Summary, error) {
	if recal == false && period == nil {
		key := path.Join(DefaultKeyspace, uid)
		gv, err := kvutil.Get[gobs.LooperState](ctx, r, key)
		if err != nil {
			return nil, fmt.Errorf("could not load looper state: %w", err)
		}
		if gv.V2.LifetimeSummary != nil {
			return gv.V2.LifetimeSummary, nil
		}
	}

	v, err := Load(ctx, uid, r)
	if err != nil {
		return nil, err
	}
	v.summary.Store(nil)
	return v.GetSummary(period), nil
}
