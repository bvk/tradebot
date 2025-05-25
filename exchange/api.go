// Copyright (c) 2023 BVK Chaitanya

package exchange

import (
	"context"
	"io"

	"github.com/bvk/tradebot/gobs"
	"github.com/shopspring/decimal"
)

type OrderID string

type Order struct {
	OrderID OrderID

	ClientOrderID string

	Side string

	CreateTime RemoteTime
	FinishTime RemoteTime

	Fee         decimal.Decimal
	FilledSize  decimal.Decimal
	FilledPrice decimal.Decimal

	Status string

	// Done is true if order is complete. DoneReason below indicates if order has
	// failed or succeeded.
	Done bool

	// When Done is true, an empty DoneReason value indicates a successfull
	// execution of the order and a non-empty DoneReason indicates a failure with
	// the reason for the failure.
	DoneReason string
}

type Ticker interface {
	PricePoint() (decimal.Decimal, RemoteTime)
}

type Product interface {
	io.Closer

	ProductID() string
	ExchangeName() string
	BaseMinSize() decimal.Decimal

	TickerCh() (ch <-chan Ticker, stopf func())
	OrderUpdatesCh() (ch <-chan *Order, stopf func())

	LimitBuy(ctx context.Context, clientOrderID string, size, price decimal.Decimal) (OrderID, error)
	LimitSell(ctx context.Context, clientOrderID string, size, price decimal.Decimal) (OrderID, error)

	Get(ctx context.Context, id OrderID) (*Order, error)
	Cancel(ctx context.Context, id OrderID) error

	// Retire(id OrderID)
}

type Exchange interface {
	io.Closer

	ExchangeName() string

	OpenProduct(ctx context.Context, productID string) (Product, error)

	GetProduct(ctx context.Context, id string) (*gobs.Product, error)
	GetOrder(ctx context.Context, id OrderID) (*Order, error)

	IsDone(status string) bool
}
