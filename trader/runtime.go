// Copyright (c) 2023 BVK Chaitanya

package trader

import (
	"context"
	"time"

	"github.com/bvk/tradebot/exchange"
	"github.com/bvkgo/kv"
)

type Messenger interface {
	SendMessage(context.Context, time.Time, string, ...interface{})
}

type Runtime struct {
	Database  kv.Database
	Product   exchange.Product
	Messenger Messenger
}
