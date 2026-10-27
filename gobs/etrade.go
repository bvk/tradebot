// Copyright (c) 2026 Deepak Vankadaru

package gobs

import "github.com/shopspring/decimal"

// ETradeOrderPlacementInfo holds the parameters of an E*TRADE PlaceOrder
// request. It is persisted (embedded in ETradeOrderEntry) so that failed-create
// reconciliation can match an order in the open-orders list by parameters when
// the server order ID is unknown (E*TRADE omits clientOrderId from list
// responses).
type ETradeOrderPlacementInfo struct {
	RequestTimeMilli int64
	Side             string
	Price            decimal.Decimal
	Qty              decimal.Decimal
	PriceType        string
	OrderTerm        string
	MarketSession    string
}

// ETradeOrderEntry is the value persisted at
// /etrade/symbol/<symbol>/clientorder/<client-order-uuid> for each order placed.
// ServerOrderID is 0 until PlaceOrder returns successfully. Placement is written
// before PlaceOrder is called so that if the process crashes before the server
// order ID is known, startup reconciliation can match the order by parameters.
type ETradeOrderEntry struct {
	ServerOrderID int64
	AccountID     string
	Placement     ETradeOrderPlacementInfo

	// Failed-create reconciliation outcome, set when a timed-out/failed place is
	// resolved (by goCancelFailedCreates or the startup reconciler). When either
	// is set the entry is a RETAINED audit record and is not auto-deleted; a
	// future subcommand can list/clean/compact these. At most one is set.
	AssumedNotCreated bool  // no matching open order was found for this create
	MatchedOrderID    int64 // this create matched (and was cancelled as) this open server order id
}

// Resolved reports whether the entry is a retained failed-create audit record
// (as opposed to a live order still being tracked).
func (e *ETradeOrderEntry) Resolved() bool {
	return e.AssumedNotCreated || e.MatchedOrderID != 0
}
