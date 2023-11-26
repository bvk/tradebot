// Copyright (c) 2023 BVK Chaitanya

package trader

import (
	"github.com/bvk/tradebot/exchange"
	"github.com/bvkgo/kv"
)

type Runtime struct {
	Database kv.Database
	Product  exchange.Product
}
