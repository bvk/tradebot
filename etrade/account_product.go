// Copyright (c) 2026 BVK Chaitanya

package etrade

import (
	"context"
	"fmt"

	"github.com/bvk/tradebot/etrade/autologin"
	"github.com/bvk/tradebot/exchange"
	"github.com/bvk/tradebot/gobs"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/visvasity/topic"
)

type AccountProduct struct {
	product *Product
	account autologin.Account
}

var _ exchange.Product = &AccountProduct{}

func (ap *AccountProduct) Close() error {
	return nil // Product is never closed.
}

func (ap *AccountProduct) ProductID() string {
	return ap.product.symbol
}

func (ap *AccountProduct) AccountName() string {
	return fmt.Sprintf("etrade/%s", ap.account.AccountName)
}

func (ap *AccountProduct) ExchangeName() string {
	return "etrade"
}

func (ap *AccountProduct) BaseMinSize() decimal.Decimal {
	return decimal.NewFromInt(1) // ETrade doesn't support fractional buys/sells.
}

func (ap *AccountProduct) GetPriceUpdates() (*topic.Receiver[exchange.PriceUpdate], error) {
	return ap.product.GetPriceUpdates()
}

func (ap *AccountProduct) GetOrderUpdates() (*topic.Receiver[exchange.OrderUpdate], error) {
	return ap.product.GetOrderUpdates(ap.account.AccountID)
}

func (ap *AccountProduct) LimitBuy(ctx context.Context, clientID uuid.UUID, size, price decimal.Decimal) (exchange.Order, error) {
	return ap.product.PlaceLimitOrder(ctx, ap.account.AccountID, clientID, "buy", size, price, nil)
}

func (ap *AccountProduct) LimitSell(ctx context.Context, clientID uuid.UUID, size, price decimal.Decimal) (exchange.Order, error) {
	return ap.product.PlaceLimitOrder(ctx, ap.account.AccountID, clientID, "sell", size, price, nil)
}

func (ap *AccountProduct) Get(ctx context.Context, serverID string) (exchange.OrderDetail, error) {
	return ap.product.Get(ctx, ap.account.AccountID, serverID)
}

func (ap *AccountProduct) Cancel(ctx context.Context, serverID string) error {
	return ap.product.Cancel(ctx, ap.account.AccountID, serverID)
}

func (ap *AccountProduct) ResolveTaxLots(ctx context.Context, serverIDs []string) ([]gobs.TaxLotID, decimal.Decimal, error) {
	return ap.product.ResolveTaxLots(ctx, ap.account.AccountID, serverIDs)
}

func (ap *AccountProduct) LimitSellTaxLots(ctx context.Context, clientID uuid.UUID, size, price decimal.Decimal, lots []gobs.TaxLotID) (exchange.Order, error) {
	return ap.product.PlaceLimitOrder(ctx, ap.account.AccountID, clientID, "sell", size, price, lots)
}
