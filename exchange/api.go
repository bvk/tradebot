// Copyright (c) 2023 BVK Chaitanya

package exchange

import (
	"context"
	"errors"
	"io"
	"os/exec"
	"time"

	"github.com/bvk/tradebot/gobs"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/visvasity/topic"
)

var (
	ErrNoFund   = errors.New("insufficient fund")
	ErrNeedsAck = errors.New("needs user acknowledgment")
)

type Messenger interface {
	SendMessage(ctx context.Context, at time.Time, fmtstr string, args ...any)
}

type DisplayRunner interface {
	RunWithDisplay(cmd *exec.Cmd) error
}

type Order interface {
	ServerID() string
	ClientID() uuid.UUID
	OrderSide() string
}

type OrderUpdate interface {
	ServerID() string
	ClientID() uuid.UUID

	CreatedAt() gobs.RemoteTime

	ExecutedFee() decimal.Decimal
	ExecutedSize() decimal.Decimal
	ExecutedValue() decimal.Decimal

	IsDone() bool
	OrderStatus() string
}

type OrderDetail interface {
	Order
	OrderUpdate

	FinishedAt() gobs.RemoteTime
}

type PriceUpdate interface {
	PricePoint() (decimal.Decimal, gobs.RemoteTime)
}

type BalanceUpdate interface {
	Balance() (string, decimal.Decimal)
}

type Product interface {
	io.Closer

	AccountName() string
	ExchangeName() string

	ProductID() string
	BaseMinSize() decimal.Decimal

	GetPriceUpdates() (*topic.Receiver[PriceUpdate], error)
	GetOrderUpdates() (*topic.Receiver[OrderUpdate], error)

	LimitBuy(ctx context.Context, clientID uuid.UUID, size, price decimal.Decimal) (Order, error)
	LimitSell(ctx context.Context, clientID uuid.UUID, size, price decimal.Decimal) (Order, error)

	Get(ctx context.Context, serverID string) (OrderDetail, error)
	Cancel(ctx context.Context, serverID string) error
}

type Exchange interface {
	ExchangeName() string

	Accounts(context.Context) ([]Account, error)
}

type Account interface {
	io.Closer

	AccountName() string
	ExchangeName() string

	// GetBalanceUpdates is a channel that sends update notifications
	// asynchronously when any asset balance (available for orders) changes on
	// the exchange.
	GetBalanceUpdates() (*topic.Receiver[BalanceUpdate], error)

	// CanDedupOnClientUUID returns true if exchange back is able to maintain
	// unique client-id constraint (eg: Coinbase). Must return false, if exchange
	// does not or cannot maintain client id uniqueness.
	//
	// For exchanges that return true, we expect that BUY/SELL orders with same
	// client-uuid will receive the existing or expired or completed, older
	// server order.
	CanDedupOnClientUUID() bool

	OpenSpotProduct(ctx context.Context, productID string) (Product, error)

	GetSpotProduct(ctx context.Context, base, quote string) (*gobs.Product, error)

	GetOrder(ctx context.Context, productID string, serverID string) (OrderDetail, error)
}

type TaxLotResolver interface {
	ResolveTaxLots(ctx context.Context, serverIDs []string) ([]gobs.TaxLotID, decimal.Decimal, error)
}

type TaxLotSeller interface {
	LimitSellTaxLots(ctx context.Context, clientID uuid.UUID, size, price decimal.Decimal, lots []gobs.TaxLotID) (Order, error)
}
