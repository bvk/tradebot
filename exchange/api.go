// Copyright (c) 2023 BVK Chaitanya

package exchange

import (
	"context"
	"io"

	"github.com/bvk/tradebot/gobs"
	"github.com/shopspring/decimal"
	"github.com/visvasity/topic"
)

type OrderID string

type Order interface {
	ServerID() string
	ClientID() string
	OrderSide() string
}

type PriceUpdate interface {
	PricePoint() (decimal.Decimal, gobs.RemoteTime)
}

type OrderUpdate interface {
	ServerID() string
	ClientID() string

	CreatedAt() gobs.RemoteTime
	UpdatedAt() gobs.RemoteTime

	ExecutedFee() decimal.Decimal
	ExecutedSize() decimal.Decimal
	ExecutedValue() decimal.Decimal

	IsDone() bool
	OrderStatus() string
}

type Product interface {
	io.Closer

	ProductID() string
	ExchangeName() string
	BaseMinSize() decimal.Decimal

	GetPriceUpdates() (*topic.Receiver[PriceUpdate], error)
	GetOrderUpdates() (*topic.Receiver[OrderUpdate], error)

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
