// Copyright (c) 2023 BVK Chaitanya

package api

import (
	"fmt"

	"github.com/bvk/tradebot/point"
)

const WallPath = "/trader/wall"

type WallRequest struct {
	ExchangeName string

	ProductID string

	Pairs []*point.Pair
}

type WallResponse struct {
	UID string
}

func (r *WallRequest) Check() error {
	if len(r.ExchangeName) == 0 {
		return fmt.Errorf("exchange name cannot be empty")
	}
	if len(r.ProductID) == 0 {
		return fmt.Errorf("product id cannot be empty")
	}
	if len(r.Pairs) == 0 {
		return fmt.Errorf("buy/sell pairs cannot be empty")
	}
	for i, p := range r.Pairs {
		if err := p.Check(); err != nil {
			return fmt.Errorf("invalid buy/sell pair %d: %w", i, err)
		}
	}
	return nil
}
