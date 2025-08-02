// Copyright (c) 2023 BVK Chaitanya

package api

import (
	"fmt"

	"github.com/bvk/tradebot/gobs"
)

const ExchangeGetOrderPath = "/exchange/get-order"

type ExchangeGetOrderRequest struct {
	ExchangeName string

	ProductID string

	OrderID string
}

type ExchangeGetOrderResponse struct {
	Error string

	Order *gobs.Order
}

func (v *ExchangeGetOrderRequest) Check() error {
	if v.ExchangeName == "" {
		return fmt.Errorf("exchange name cannot be empty")
	}
	if v.ProductID == "" {
		return fmt.Errorf("product id cannot be empty")
	}
	if v.OrderID == "" {
		return fmt.Errorf("order id cannot be empty")
	}
	return nil
}
