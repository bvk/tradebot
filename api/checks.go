// Copyright (c) 2023 BVK Chaitanya

package api

import "fmt"

func (r *LoopRequest) Check() error {
	if err := r.Buy.Check(); err != nil {
		return err
	}
	if err := r.Sell.Check(); err != nil {
		return err
	}
	if r.Buy.Side() != "BUY" {
		return fmt.Errorf("buy point falls on wrong side")
	}
	if r.Sell.Side() != "SELL" {
		return fmt.Errorf("sell point falls on wrong side")
	}
	if r.Buy.Size.LessThan(r.Sell.Size) {
		return fmt.Errorf("buy size cannot be smaller than sell size")
	}
	if r.Buy.Price.GreaterThanOrEqual(r.Sell.Price) {
		return fmt.Errorf("buy price cannot be greater or equal to the sell price")
	}
	return nil
}

func (r *WallRequest) Check() error {
	// TODO
	return nil
}
