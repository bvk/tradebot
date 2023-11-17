// Copyright (c) 2023 BVK Chaitanya

package api

import (
	"github.com/bvk/tradebot/gobs"
)

const ExchangeGetOrderPath = "/exchange/get-order"

type ExchangeGetOrderRequest struct {
	Name string

	OrderID string
}

type ExchangeGetOrderResponse struct {
	Error string

	Order *gobs.Order
}
