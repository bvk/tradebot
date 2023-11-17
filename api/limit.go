// Copyright (c) 2023 BVK Chaitanya

package api

import (
	"fmt"

	"github.com/bvk/tradebot/point"
)

const LimitPath = "/trader/limit"

type LimitRequest struct {
	ProductID string

	TradePoint *point.Point
}

type LimitResponse struct {
	UID string

	Side string
}

func (r *LimitRequest) Check() error {
	if len(r.ProductID) == 0 {
		return fmt.Errorf("product id cannot be empty")
	}
	if err := r.TradePoint.Check(); err != nil {
		return fmt.Errorf("invalid trade point: %w", err)
	}
	return nil
}
