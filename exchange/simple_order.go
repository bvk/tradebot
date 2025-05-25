// Copyright (c) 2025 BVK Chaitanya

package exchange

import "github.com/shopspring/decimal"

type SimpleOrder struct {
	ServerOrderID OrderID

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
