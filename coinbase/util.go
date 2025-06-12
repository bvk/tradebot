// Copyright (c) 2023 BVK Chaitanya

package coinbase

import (
	"fmt"
	"slices"
	"strings"

	"github.com/bvk/tradebot/coinbase/internal"
	"github.com/bvk/tradebot/exchange"
	"github.com/bvk/tradebot/gobs"
	"github.com/google/uuid"
)

var doneStatuses []string = []string{
	"FILLED", "CANCELLED", "EXPIRED", "FAILED",
}

var readyStatuses []string = []string{
	"OPEN", "FILLED", "CANCELLED", "EXPIRED", "FAILED",
}

func gobOrderFromOrder(v *internal.Order) *gobs.Order {
	order := &gobs.Order{
		ServerOrderID: v.OrderID,
		ClientOrderID: v.ClientOrderID,
		CreateTime:    gobs.RemoteTime{Time: v.CreatedTime.Time},
		FinishTime:    gobs.RemoteTime{Time: v.LastFillTime.Time},
		Side:          v.Side,
		FilledFee:     v.TotalFees.Decimal,
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

func exchangeOrderFromOrder(v *internal.Order) (*exchange.SimpleOrder, error) {
	cuuid, err := uuid.Parse(v.ClientOrderID)
	if err != nil {
		return nil, fmt.Errorf("could not parse client order id as uuid: %w", err)
	}
	order := &exchange.SimpleOrder{
		ClientUUID:    cuuid,
		ServerOrderID: v.OrderID,
		CreateTime:    gobs.RemoteTime{Time: v.CreatedTime.Time},
		FinishTime:    gobs.RemoteTime{Time: v.LastFillTime.Time},
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
	return order, nil
}

func exchangeOrderFromEvent(event *internal.OrderEvent) (*exchange.SimpleOrder, error) {
	cuuid, err := uuid.Parse(event.ClientOrderID)
	if err != nil {
		return nil, fmt.Errorf("could not parse client order id from event as uuid: %w", err)
	}
	order := &exchange.SimpleOrder{
		ServerOrderID: event.OrderID,
		ClientUUID:    cuuid,
		CreateTime:    gobs.RemoteTime{Time: event.CreatedTime.Time},
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
	return order, nil
}

func compareFilledSize(a, b *internal.Order) int {
	return a.FilledSize.Decimal.Cmp(b.FilledSize.Decimal)
}

func compareLastFillTime(a, b *internal.Order) int {
	return a.LastFillTime.Time.Compare(b.LastFillTime.Time)
}

func compareCreatedTime(a, b *internal.Order) int {
	return a.CreatedTime.Time.Compare(b.CreatedTime.Time)
}

func compareOrderID(a, b *internal.Order) int {
	return strings.Compare(a.OrderID, b.OrderID)
}

func compareInternalOrder(a, b *internal.Order) int {
	if a.OrderID == b.OrderID {
		if v := compareFilledSize(a, b); v != 0 {
			return v
		}
		return compareLastFillTime(a, b)
	}
	if a.OrderID < b.OrderID {
		return -1
	}
	return 1
}

func equalLastFillTime(a, b *internal.Order) bool {
	return compareLastFillTime(a, b) == 0
}
