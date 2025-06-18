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

func GetPriceMap(ctx context.Context) (map[string]decimal.Decimal, error) {
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
		if strings.HasSuffix(pi.Market, "USDT") {
			ccy := strings.TrimSuffix(pi.Market, "USDT")
			marketPriceMap[ccy] = pi.Price
		}
	}
	return marketPriceMap, nil
}
