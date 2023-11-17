// Copyright (c) 2023 BVK Chaitanya

package api

import "github.com/bvk/tradebot/point"

const LoopPath = "/trader/loop"

type LoopRequest struct {
	Product string

	Buy  point.Point
	Sell point.Point
}

type LoopResponse struct {
	UID string
}
