// Copyright (c) 2023 BVK Chaitanya

package exchange

import (
	"context"
	"io"
	"time"

	"github.com/bvk/tradebot/gobs"
	"github.com/shopspring/decimal"
)

type OrderID string

type Order struct {
	OrderID OrderID

	ClientOrderID string

	Side string

	CreateTime RemoteTime

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

type Ticker struct {
	Timestamp RemoteTime
	Price     decimal.Decimal
}

type Product interface {
	io.Closer

	ProductID() string
	ExchangeName() string
	BaseMinSize() decimal.Decimal

	TickerCh(context.Context) <-chan *Ticker

	// OrderUpdatesCh returns a channel that receives order notifications/updates
	// for all order on this product. Channel is closed when the input context is
	// canceled.
	OrderUpdatesCh(context.Context) <-chan *Order

	LimitBuy(ctx context.Context, clientOrderID string, size, price decimal.Decimal) (OrderID, error)
	LimitSell(ctx context.Context, clientOrderID string, size, price decimal.Decimal) (OrderID, error)

	Get(ctx context.Context, id OrderID) (*Order, error)
	Cancel(ctx context.Context, id OrderID) error

	// Retire(id OrderID)
}

type Exchange interface {
	io.Closer

	OpenProduct(ctx context.Context, productID string) (Product, error)

	GetProduct(ctx context.Context, id string) (*gobs.Product, error)
	GetOrder(ctx context.Context, id OrderID) (*Order, error)
	GetCandles(ctx context.Context, productID string, from time.Time) ([]*gobs.Candle, error)

	IsDone(status string) bool
}
