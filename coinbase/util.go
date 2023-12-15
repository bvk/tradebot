// Copyright (c) 2023 BVK Chaitanya

package coinbase

import (
	"slices"

	"github.com/bvk/tradebot/coinbase/internal"
	"github.com/bvk/tradebot/exchange"
)

var doneStatuses []string = []string{
	"FILLED", "CANCELLED", "EXPIRED", "FAILED",
}

var readyStatuses []string = []string{
	"OPEN", "FILLED", "CANCELLED", "EXPIRED", "FAILED",
}

func exchangeOrderFromOrder(v *internal.Order) *exchange.Order {
	order := &exchange.Order{
		ClientOrderID: v.ClientOrderID,
		OrderID:       exchange.OrderID(v.OrderID),
		CreateTime:    exchange.RemoteTime{Time: v.CreatedTime.Time},
		Side:          v.Side,
		Fee:           v.TotalFees.Decimal,
		FilledSize:    v.FilledSize.Decimal,
		FilledPrice:   v.AvgFilledPrice.Decimal,
		Status:        v.Status,
		Done:          slices.Contains(doneStatuses, v.Status),
	}
	if order.Done && order.Status != "FILLED" {
		order.DoneReason = order.Status
	}
	return order
}

func exchangeOrderFromEvent(event *internal.OrderEvent) *exchange.Order {
	order := &exchange.Order{
		OrderID:       exchange.OrderID(event.OrderID),
		ClientOrderID: event.ClientOrderID,
		CreateTime:    exchange.RemoteTime{Time: event.CreatedTime.Time},
		Side:          event.OrderSide,
		Status:        event.Status,
		Done:          slices.Contains(doneStatuses, event.Status),
		FilledSize:    event.CumulativeQuantity.Decimal,
		FilledPrice:   event.AvgPrice.Decimal,
		Fee:           event.TotalFees.Decimal,
	}
	if order.Done && event.Status != "FILLED" {
		order.DoneReason = event.Status
	}
	return order
}

func compareLastFillTime(a, b *internal.Order) int {
	return a.LastFillTime.Time.Compare(b.LastFillTime.Time)
}

func compareCreatedTime(a, b *internal.Order) int {
	return a.CreatedTime.Time.Compare(b.CreatedTime.Time)
}
