// Copyright (c) 2023 BVK Chaitanya

package api

import (
	"fmt"

	"github.com/bvk/tradebot/gobs"
)

const ExchangeGetProductPath = "/exchange/get-product"

type ExchangeGetProductRequest struct {
	AccountName string

	ProductType string // SPOT, MARGIN, etc. Only SPOT is supported.

	Base  string
	Quote string
}

type ExchangeGetProductResponse struct {
	Error string

	Product *gobs.Product
}

func (v *ExchangeGetProductRequest) Check() error {
	if v.AccountName == "" {
		return fmt.Errorf("account name cannot be empty")
	}
	if v.ProductType != "SPOT" {
		return fmt.Errorf("only spot product types are supported")
	}
	if v.Base == "" || v.Quote == "" {
		return fmt.Errorf("Base and Quote names are both required")
	}
	return nil
}
