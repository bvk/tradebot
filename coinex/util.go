// Copyright (c) 2025 BVK Chaitanya

package coinex

import (
	"context"
	"net/http"
	"net/url"
	"path"
	"strings"

	"github.com/shopspring/decimal"
)

func GetPriceMap(productPriceMap map[string]decimal.Decimal) map[string]decimal.Decimal {
	cryptoPriceMap := make(map[string]decimal.Decimal)
	for product, price := range productPriceMap {
		if strings.HasSuffix(product, "USDT") {
			ccy := strings.TrimSuffix(product, "USDT")
			cryptoPriceMap[ccy] = price
		}
	}
	return cryptoPriceMap
}

func GetProductPriceMap(ctx context.Context) (map[string]decimal.Decimal, error) {
	type PriceInfo struct {
		Market string          `json:"market"`
		Price  decimal.Decimal `json:"last"`
	}
	type Response struct {
		Code    int          `json:"code"`
		Message string       `json:"message"`
		Data    []*PriceInfo `json:"data"`
	}
	opts := new(Options)
	opts.setDefaults()
	addrURL := &url.URL{
		Scheme: RestURL.Scheme,
		Host:   RestURL.Host,
		Path:   path.Join(RestURL.Path, "/spot/ticker"),
	}
	resp := new(Response)
	if err := httpGetJSON(ctx, http.DefaultClient, addrURL, resp, opts); err != nil {
		return nil, err
	}
	marketPriceMap := make(map[string]decimal.Decimal)
	for _, pi := range resp.Data {
		marketPriceMap[pi.Market] = pi.Price
	}
	return marketPriceMap, nil
}
