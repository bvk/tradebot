// Copyright (c) 2025 BVK Chaitanya

package waller

import (
	"context"
	"fmt"
	"path"

	"github.com/bvk/tradebot/gobs"
	"github.com/bvk/tradebot/kvutil"
	"github.com/bvk/tradebot/timerange"
	"github.com/bvkgo/kv"
)

func Summary(ctx context.Context, r kv.Reader, uid string, period *timerange.Range) (*gobs.Summary, error) {
	if period == nil {
		key := path.Join(DefaultKeyspace, uid)
		gv, err := kvutil.Get[gobs.WallerState](ctx, r, key)
		if err != nil {
			return nil, fmt.Errorf("could not load waller state: %w", err)
		}
		if gv.V2.LifetimeSummary != nil {
			return gv.V2.LifetimeSummary, nil
		}
	}
	v, err := Load(ctx, uid, r)
	if err != nil {
		return nil, err
	}
	return v.GetSummary(period), nil
}
