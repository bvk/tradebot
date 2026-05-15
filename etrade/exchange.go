// Copyright (c) 2026 Deepak Vankadaru

package etrade

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"

	"github.com/bvk/tradebot/etrade/internal"
	"github.com/bvk/tradebot/exchange"
	"github.com/bvk/tradebot/gobs"
	"github.com/bvk/tradebot/syncmap"
	"github.com/bvkgo/kv"
	"github.com/shopspring/decimal"
	"github.com/visvasity/topic"
)

// Exchange implements exchange.Exchange for E*TRADE equity markets.
type Exchange struct {
	db     kv.Database
	client *Client

	productMap syncmap.Map[string, *Product]
}

var _ exchange.Exchange = &Exchange{}

// NewExchange creates an Exchange, verifies credentials, and starts the
// background polling goroutines in the underlying client.
func NewExchange(ctx context.Context, db kv.Database, creds *Credentials, opts *Options) (_ *Exchange, status error) {
	client, err := New(ctx, creds, opts)
	if err != nil {
		return nil, err
	}
	defer func() {
		if status != nil {
			client.Close()
		}
	}()

	v := &Exchange{
		db:     db,
		client: client,
	}
	return v, nil
}

func (v *Exchange) Close() error {
	if err := v.client.Close(); err != nil {
		slog.Error("etrade: could not close client (ignored)", "err", err)
	}
	return nil
}

func (v *Exchange) ExchangeName() string {
	return "etrade"
}

// CanDedupOnClientUUID returns false because E*TRADE does not maintain a
// client-UUID uniqueness constraint; deduplication is handled locally.
func (v *Exchange) CanDedupOnClientUUID() bool {
	return false
}

func (v *Exchange) GetBalanceUpdates() (*topic.Receiver[exchange.BalanceUpdate], error) {
	fn := func(b *internal.Balance) exchange.BalanceUpdate { return b }
	return topic.SubscribeFunc(v.client.balancesTopic, fn, 0, true)
}

// OpenSpotProduct opens (or returns a cached) Product for the given equity
// symbol. E*TRADE does not expose a product catalogue API, so any non-empty
// symbol is accepted; invalid symbols will surface as errors during order
// placement or price polling.
func (v *Exchange) OpenSpotProduct(ctx context.Context, productID string) (exchange.Product, error) {
	if p, ok := v.productMap.Load(productID); ok {
		return p, nil
	}
	p, err := NewProduct(ctx, v.db, v.client, productID)
	if err != nil {
		return nil, err
	}
	if existing, loaded := v.productMap.LoadOrStore(productID, p); loaded {
		p.Close()
		p = existing
	}
	return p, nil
}

// GetSpotProduct returns metadata for an equity symbol. E*TRADE equity markets
// are always USD-denominated; quote must be "USD". The current mid-price is
// fetched from the quotes endpoint.
func (v *Exchange) GetSpotProduct(ctx context.Context, base, quote string) (*gobs.Product, error) {
	if quote != "USD" {
		return nil, fmt.Errorf("etrade: only USD is supported as quote currency")
	}
	symbol := base
	var price decimal.Decimal
	quotes, err := v.client.GetQuotes(ctx, []string{symbol})
	if err != nil {
		slog.Warn("etrade: could not fetch quote for product (using zero price)", "symbol", symbol, "err", err)
	} else if len(quotes) > 0 {
		price, _ = quotes[0].PricePoint()
	}
	return &gobs.Product{
		ProductID:       symbol,
		Status:          "online",
		BaseMinSize:     decimal.NewFromInt(1),
		Price:           price,
		BaseCurrencyID:  base,
		QuoteCurrencyID: quote,
	}, nil
}

// GetOrder fetches an order by server ID. If the product is currently open,
// the call is routed through it so that the ClientUUID is restored from the
// in-memory counterIDMap. Otherwise the order is fetched directly from the
// client without UUID restoration.
func (v *Exchange) GetOrder(ctx context.Context, productID string, serverID string) (exchange.OrderDetail, error) {
	if p, ok := v.productMap.Load(productID); ok {
		return p.Get(ctx, serverID)
	}
	orderID, err := strconv.ParseInt(serverID, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("etrade: invalid server order id %q: %w", serverID, err)
	}
	return v.client.GetOrder(ctx, orderID)
}
