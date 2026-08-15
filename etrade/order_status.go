// Copyright (c) 2026 Deepak Vankadaru

package etrade

import (
	"strings"
	"sync"

	"github.com/bvk/tradebot/etrade/internal"
	"github.com/shopspring/decimal"
)

// orderPlacementInfo holds the parameters of a PlaceOrder request. Used by
// goCancelFailedCreates to match the order in the open-orders list when the
// server order ID is unknown (E*TRADE omits clientOrderId from list responses).
// Fields are exported so gob can serialize them when embedded in orderEntry.
type orderPlacementInfo struct {
	RequestTimeMilli int64
	Side             string
	Price            decimal.Decimal
	Qty              decimal.Decimal
	PriceType        string
	OrderTerm        string
	MarketSession    string
}

// clientIDStatus tracks the state of a single order keyed by the client order
// UUID. It is protected by its own mutex because LimitBuy/LimitSell,
// goWatchOrderUpdates, and goCancelFailedCreates all access it concurrently.
type clientIDStatus struct {
	mu sync.Mutex

	// err is set when order placement failed permanently. Subsequent calls with
	// the same clientOrderUUID return this error immediately.
	err error

	// etrade account id.
	accountID string

	// order holds the most recent E*TRADE order state. Nil until placement
	// succeeds or a matching update arrives.
	order *internal.Order

	// placement holds the original request parameters, set before PlaceOrder is
	// called and used for recovery matching if the call times out or crashes.
	placement orderPlacementInfo
}

func absInt64(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

// matchOpenOrder finds the first order in candidates that matches the given
// symbol and placement parameters, within a 60-second window of the request time.
// Used to recover orders when the server ID is unknown (E*TRADE omits
// clientOrderId from list responses so we cannot match by that field).
func matchOpenOrder(candidates []*internal.Order, accountID, symbol string, p orderPlacementInfo) *internal.Order {
	const matchWindowMilli = 60_000
	for _, o := range candidates {
		if o.AccountID != accountID {
			continue
		}
		if o.Symbol != symbol {
			continue
		}
		if !strings.EqualFold(o.Side, p.Side) {
			continue
		}
		if !strings.EqualFold(o.PriceType, p.PriceType) {
			continue
		}
		if !strings.EqualFold(o.OrderTerm, p.OrderTerm) {
			continue
		}
		if !o.LimitPrice.Equal(p.Price) {
			continue
		}
		if !o.OrderedQty.Equal(p.Qty) {
			continue
		}
		if absInt64(o.PlacedTimeMilli-p.RequestTimeMilli) > matchWindowMilli {
			continue
		}
		return o
	}
	return nil
}
