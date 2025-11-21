// Copyright (c) 2023 BVK Chaitanya

package api

import (
	"fmt"

	"github.com/bvk/tradebot/point"
)

const LoopPath = "/trader/loop"

type LoopRequest struct {
	ExchangeName string

	ProductID string

	Buy  *point.Point
	Sell *point.Point

	Pause bool
}

type LoopResponse struct {
	UID string
}

func (r *LoopRequest) Check() error {
	if len(r.ExchangeName) == 0 {
		return fmt.Errorf("exchange name cannot be empty")
	}
	if len(r.ProductID) == 0 {
		return fmt.Errorf("product id cannot be empty")
	}
	if err := r.Buy.Check(); err != nil {
		return fmt.Errorf("invalid buy point: %w", err)
	}
	if r.Buy.Side() != "BUY" {
		return fmt.Errorf("invalid buy point side")
	}
	if err := r.Sell.Check(); err != nil {
		return fmt.Errorf("invalid sell point: %w", err)
	}
	if r.Sell.Side() != "SELL" {
		return fmt.Errorf("invalid sell point side")
	}
	return nil
}
