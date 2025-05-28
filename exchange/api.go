// Copyright (c) 2023 BVK Chaitanya

package exchange

import (
	"context"
	"io"

	"github.com/bvk/tradebot/gobs"
	"github.com/shopspring/decimal"
)

type OrderID string

type Ticker interface {
	PricePoint() (decimal.Decimal, RemoteTime)
}

type Product interface {
	io.Closer

	ProductID() string
	ExchangeName() string
	BaseMinSize() decimal.Decimal

	TickerCh() (ch <-chan Ticker, stopf func())
	OrderUpdatesCh() (ch <-chan *SimpleOrder, stopf func())

	LimitBuy(ctx context.Context, clientOrderID string, size, price decimal.Decimal) (OrderID, error)
	LimitSell(ctx context.Context, clientOrderID string, size, price decimal.Decimal) (OrderID, error)

	Get(ctx context.Context, id OrderID) (*SimpleOrder, error)
	Cancel(ctx context.Context, id OrderID) error

	// Retire(id OrderID)
}

type Exchange interface {
	io.Closer

	ExchangeName() string

	OpenSpotProduct(ctx context.Context, productID string) (Product, error)

	GetSpotProduct(ctx context.Context, base, quote string) (*gobs.Product, error)

	GetOrder(ctx context.Context, productID string, orderID OrderID) (*SimpleOrder, error)
}
