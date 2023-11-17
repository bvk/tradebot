// Copyright (c) 2023 BVK Chaitanya

package api

import "github.com/bvk/tradebot/point"

const WallPath = "/trader/wall"

type WallRequest struct {
	Product string

	BuySellPoints [][2]*point.Point
}

type WallResponse struct {
	UID string
}
