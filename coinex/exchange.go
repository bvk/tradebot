// Copyright (c) 2025 BVK Chaitanya

package coinex

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"slices"
	"strconv"

	"github.com/bvk/tradebot/coinex/internal"
	"github.com/bvk/tradebot/exchange"
	"github.com/bvk/tradebot/gobs"
	"github.com/bvk/tradebot/syncmap"
)

type Exchange struct {
	client *Client

	markets []*internal.MarketStatus

	productMap syncmap.Map[string, *Product]
}

var _ exchange.Exchange = &Exchange{}

func NewExchange(ctx context.Context, key, secret string, opts *Options) (_ *Exchange, status error) {
	client, err := New(ctx, key, secret, opts)
	if err != nil {
		return nil, err
	}
	defer func() {
		if status != nil {
			client.Close()
		}
	}()

	markets, err := client.GetMarkets(ctx)
	if err != nil {
		return nil, err
	}

	v := &Exchange{
		client:  client,
		markets: markets,
	}
	return v, nil
}

func (v *Exchange) Close() error {
	if err := v.client.Close(); err != nil {
		slog.Error("could not close coinex client (ignored)", "err", err)
	}
	return nil
}

func (v *Exchange) ExchangeName() string {
	return "coinex"
}

func (v *Exchange) CanDedupOnClientUUID() bool {
	return false
}

func (v *Exchange) OpenSpotProduct(ctx context.Context, productID string) (exchange.Product, error) {
	if p, ok := v.productMap.Load(productID); ok {
		return p, nil
	}
	index := slices.IndexFunc(v.markets, func(m *internal.MarketStatus) bool {
		return productID == m.Market
	})
	if index == -1 {
		return nil, fmt.Errorf("productID name %q not found: %w", productID, os.ErrNotExist)
	}
	mstatus := v.markets[index]
	if !mstatus.IsAPITradingAvailable {
		return nil, fmt.Errorf("api trading is not available for productID %q", productID)
	}
	p, err := NewProduct(ctx, v.client, productID)
	if err != nil {
		return nil, err
	}
	if pp, loaded := v.productMap.LoadOrStore(productID, p); loaded {
		p.Close()
		p = pp
	}
	return p, nil
}

func (v *Exchange) GetSpotProduct(ctx context.Context, base, quote string) (*gobs.Product, error) {
	if quote != "USDT" {
		return nil, fmt.Errorf("only USDT is supported as quote currency")
	}
	market := base + quote
	index := slices.IndexFunc(v.markets, func(m *internal.MarketStatus) bool {
		return market == m.Market
	})
	if index == -1 {
		return nil, fmt.Errorf("market name %q not found: %w", market, os.ErrNotExist)
	}
	mstatus := v.markets[index]
	if !mstatus.IsAPITradingAvailable {
		return nil, fmt.Errorf("api trading is not available for market %q", market)
	}
	minfo, err := v.client.GetMarketInfo(ctx, mstatus.Market)
	if err != nil {
		return nil, err
	}
	p := &gobs.Product{
		ProductID:       mstatus.Market,
		Status:          mstatus.Status,
		BaseMinSize:     mstatus.MinAmount,
		Price:           minfo.LastPrice,
		BaseCurrencyID:  base,
		QuoteCurrencyID: quote,
	}
	return p, nil
}

func (v *Exchange) GetOrder(ctx context.Context, productID string, orderID string) (exchange.OrderDetail, error) {
	id, err := strconv.ParseInt(string(orderID), 10, 64)
	if err != nil {
		return nil, err
	}
	order, err := v.client.GetOrder(ctx, productID, id)
	if err != nil {
		return nil, err
	}
	return order, nil
}
