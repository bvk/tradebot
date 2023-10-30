// Copyright (c) 2023 BVK Chaitanya

package api

import "fmt"

func (r *LoopRequest) Check() error {
	if r.BuySize.LessThan(r.SellSize) {
		return fmt.Errorf("buy size cannot be smaller than sell size")
	}
	if r.BuyPrice.GreaterThanOrEqual(r.SellPrice) {
		return fmt.Errorf("buy price cannot be greater or equal to the sell price")
	}
	return nil
}

func (r *WallRequest) Check() error {
	// TODO
	return nil
}
