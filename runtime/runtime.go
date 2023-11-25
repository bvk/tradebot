// Copyright (c) 2023 BVK Chaitanya

package runtime

import (
	"github.com/bvk/tradebot/exchange"
	"github.com/bvkgo/kv"
)

type Runtime struct {
	Database kv.Database
	Product  exchange.Product
}
