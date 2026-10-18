// Copyright (c) 2023 BVK Chaitanya

package api

import (
	"fmt"

	"github.com/bvk/tradebot/gobs"
)

const ExchangeGetOrderPath = "/exchange/get-order"

type ExchangeGetOrderRequest struct {
	AccountName string

	ProductID string

	OrderID string
}

type ExchangeGetOrderResponse struct {
	Error string

	Order *gobs.Order
}

func (v *ExchangeGetOrderRequest) Check() error {
	if v.AccountName == "" {
		return fmt.Errorf("account name cannot be empty")
	}
	if v.ProductID == "" {
		return fmt.Errorf("product id cannot be empty")
	}
	if v.OrderID == "" {
		return fmt.Errorf("order id cannot be empty")
	}
	return nil
}
