// Copyright (c) 2024 BVK Chaitanya

package limiter

import (
	"context"

	"github.com/bvk/tradebot/ctxutil"
	"github.com/bvk/tradebot/exchange"
	"github.com/bvkgo/kv"
)

func RunBackgroundTasks(cg *ctxutil.CloseGroup, db kv.Database, ex exchange.Account) {
	cg.Go(func(ctx context.Context) {
		fixFinishTimes(ctx, db, ex)
	})
}
