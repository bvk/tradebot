// Copyright (c) 2026 BVK Chaitanya

package etrade

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"

	"github.com/bvk/tradebot/etrade/autologin"
	"github.com/bvk/tradebot/etrade/internal"
	"github.com/bvk/tradebot/exchange"
	"github.com/bvk/tradebot/gobs"
	"github.com/bvk/tradebot/syncmap"
	"github.com/visvasity/topic"
)

type ExchangeAccount struct {
	exchange   *Exchange
	account    autologin.Account
	productMap syncmap.Map[string, *AccountProduct]
}

var _ exchange.Account = &ExchangeAccount{}

func (ea *ExchangeAccount) Close() error {
	return nil // Exchange is never closed.
}

func (ea *ExchangeAccount) AccountName() string {
	return fmt.Sprintf("etrade/%s", ea.account.AccountName)
}

func (ea *ExchangeAccount) ExchangeName() string {
	return "etrade"
}

// CanDedupOnClientUUID returns false because E*TRADE does not maintain a
// client-UUID uniqueness constraint; deduplication is handled locally.
func (a *ExchangeAccount) CanDedupOnClientUUID() bool {
	return false
}

func (ea *ExchangeAccount) GetSpotProduct(ctx context.Context, base, quote string) (*gobs.Product, error) {
	return ea.exchange.getSpotProduct(ctx, base, quote)
}

func (a *ExchangeAccount) GetBalanceUpdates() (*topic.Receiver[exchange.BalanceUpdate], error) {
	balancesTopic, ok := a.exchange.client.balancesTopicMap.Load(a.account.AccountID)
	if !ok {
		slog.Error("etrade: could not find balance updates topic for account id", "account-id", a.account.AccountID)
		return nil, fmt.Errorf("etrade: invalid account id %s", a.account.AccountID)
	}
	fn := func(b *internal.Balance) exchange.BalanceUpdate {
		if b.AccountID != a.account.AccountID {
			return nil
		}
		return b
	}
	return topic.SubscribeFunc(balancesTopic, fn, 0, true)
}

// OpenSpotProduct opens (or returns a cached) Product for the given equity
// symbol. E*TRADE does not expose a product catalogue API, so any non-empty
// symbol is accepted; invalid symbols will surface as errors during order
// placement or price polling.
func (ea *ExchangeAccount) OpenSpotProduct(ctx context.Context, productID string) (exchange.Product, error) {
	if ap, ok := ea.productMap.Load(productID); ok {
		return ap, nil
	}
	p, err := ea.exchange.openSpotProduct(ctx, productID)
	if err != nil {
		return nil, err
	}
	ap := &AccountProduct{product: p, account: ea.account}
	if existing, loaded := ea.productMap.LoadOrStore(productID, ap); loaded {
		ap.Close()
		ap = existing
	}
	return ap, nil
}

// GetOrder fetches an order by server ID. If the product is currently open,
// the call is routed through it so that the ClientUUID is restored from the
// in-memory counterIDMap. Otherwise the order is fetched directly from the
// client without UUID restoration.
func (ea *ExchangeAccount) GetOrder(ctx context.Context, productID string, serverID string) (exchange.OrderDetail, error) {
	if ap, ok := ea.productMap.Load(productID); ok {
		return ap.Get(ctx, serverID)
	}
	orderID, err := strconv.ParseInt(serverID, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("etrade: invalid server order id %q: %w", serverID, err)
	}
	return ea.exchange.client.GetOrder(ctx, ea.account.AccountID, orderID)
}
