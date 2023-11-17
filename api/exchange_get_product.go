// Copyright (c) 2023 BVK Chaitanya

package api

import "github.com/bvk/tradebot/gobs"

const ExchangeGetProductPath = "/exchange/get-product"

type ExchangeGetProductRequest struct {
	ExchangeName string
	ProductID    string
}

type ExchangeGetProductResponse struct {
	Error string

	Product *gobs.Product
}
