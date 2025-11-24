// Copyright (c) 2025 BVK Chaitanya

package coinbase

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/shopspring/decimal"
)

func GetPriceMap(productPriceMap map[string]decimal.Decimal) map[string]decimal.Decimal {
	cryptoPriceMap := make(map[string]decimal.Decimal)
	for product, price := range productPriceMap {
		if strings.HasSuffix(product, "-USD") {
			ccy := strings.TrimSuffix(product, "-USD")
			cryptoPriceMap[ccy] = price
		}
	}
	return cryptoPriceMap
}

func GetProductPriceMap(ctx context.Context) (map[string]decimal.Decimal, error) {
	addrURL := &url.URL{
		Scheme: "https",
		Host:   "api.coinbase.com",
		Path:   "/api/v3/brokerage/market/products",
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, addrURL.String(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("http.Get failed with non-ok status %d", resp.StatusCode)
	}
	type Product struct {
		ProductID string          `json:"product_id"`
		Price     decimal.Decimal `json:"price"`
	}
	type Response struct {
		Products []*Product `json:"products"`
	}
	reply := new(Response)
	if err := json.NewDecoder(resp.Body).Decode(reply); err != nil {
		return nil, err
	}
	marketPriceMap := make(map[string]decimal.Decimal)
	for _, p := range reply.Products {
		marketPriceMap[p.ProductID] = p.Price
	}
	return marketPriceMap, nil
}
