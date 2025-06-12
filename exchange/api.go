// Copyright (c) 2023 BVK Chaitanya

package exchange

import (
	"context"
	"io"

	"github.com/bvk/tradebot/gobs"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/visvasity/topic"
)

type Order interface {
	ServerID() string
	ClientID() uuid.UUID
	OrderSide() string
}

type OrderUpdate interface {
	ServerID() string
	ClientID() uuid.UUID

	CreatedAt() gobs.RemoteTime
	UpdatedAt() gobs.RemoteTime

	ExecutedFee() decimal.Decimal
	ExecutedSize() decimal.Decimal
	ExecutedValue() decimal.Decimal

	IsDone() bool
	OrderStatus() string
}

type OrderDetail interface {
	ServerID() string
	ClientID() uuid.UUID

	OrderSide() string
	CreatedAt() gobs.RemoteTime
	FinishedAt() gobs.RemoteTime

	ExecutedFee() decimal.Decimal
	ExecutedSize() decimal.Decimal
	ExecutedValue() decimal.Decimal

	IsDone() bool
	OrderStatus() string
}

type PriceUpdate interface {
	PricePoint() (decimal.Decimal, gobs.RemoteTime)
}

type Product interface {
	io.Closer

	ProductID() string
	ExchangeName() string
	BaseMinSize() decimal.Decimal

	GetPriceUpdates() (*topic.Receiver[PriceUpdate], error)
	GetOrderUpdates() (*topic.Receiver[OrderUpdate], error)

	LimitBuy(ctx context.Context, clientID uuid.UUID, size, price decimal.Decimal) (Order, error)
	LimitSell(ctx context.Context, clientID uuid.UUID, size, price decimal.Decimal) (Order, error)

	Get(ctx context.Context, serverID string) (OrderDetail, error)
	Cancel(ctx context.Context, serverID string) error
}

type Exchange interface {
	io.Closer

	ExchangeName() string

	OpenSpotProduct(ctx context.Context, productID string) (Product, error)

	GetSpotProduct(ctx context.Context, base, quote string) (*gobs.Product, error)

	GetOrder(ctx context.Context, productID string, serverID string) (OrderDetail, error)
}
