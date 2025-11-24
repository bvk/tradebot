// Copyright (c) 2025 BVK Chaitanya

package waller

import (
	"context"

	"github.com/bvk/tradebot/gobs"
	"github.com/bvk/tradebot/timerange"
	"github.com/bvkgo/kv"
)

func Summary(ctx context.Context, r kv.Reader, uid string, period *timerange.Range) (*gobs.Summary, error) {
	v, err := Load(ctx, uid, r)
	if err != nil {
		return nil, err
	}
	return v.GetSummary(period), nil
}
