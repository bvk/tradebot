// Copyright (c) 2023 BVK Chaitanya

package coinbase

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/bvkgo/topic/v2"
	"github.com/bvkgo/tradebot/exchange"
)

var doneStatuses []string = []string{
	"FILLED", "CANCELLED", "EXPIRED", "FAILED",
}

type orderData struct {
	serverTime time.Time

	topic *topic.Topic[*exchange.Order]

	ch <-chan *exchange.Order
}

func newOrderData() *orderData {
	d := &orderData{
		topic: topic.New[*exchange.Order](),
	}
	_, d.ch, _ = d.topic.Subscribe(0)
	return d
}

func (d *orderData) Close() {
	d.topic.Close()
}

func (d *orderData) status() string {
	s, _ := topic.Recent(d.topic)
	return s.Status
}

func (d *orderData) waitForOpen(ctx context.Context) (string, error) {
	wanted := append(doneStatuses, "OPEN")

	r, ch, _ := d.topic.Subscribe(1)
	defer r.Unsubscribe()

	v, _ := topic.Recent(d.topic)
	for v == nil || !slices.Contains(wanted, v.Status) {
		select {
		case <-ctx.Done():
			return "", context.Cause(ctx)
		case v = <-ch:
			if v == nil {
				return "", fmt.Errorf("unexpected: topic closed")
			}
		}
	}

	return v.Status, nil
}

func (d *orderData) websocketUpdate(serverTime time.Time, event *OrderEventType) {
	if serverTime.Before(d.serverTime) {
		slog.Warn("out of order user channel update is ignored")
		return
	}

	if last, ok := topic.Recent(d.topic); ok && last.Done {
		return
	}

	order := &exchange.Order{
		OrderID:       exchange.OrderID(event.OrderID),
		ClientOrderID: event.ClientOrderID,
		CreateTime:    exchange.RemoteTime(event.CreatedTime.Time),
		Side:          event.OrderSide,
		Status:        event.Status,
		Done:          slices.Contains(doneStatuses, event.Status),
	}

	if order.Done && event.Status != "FILLED" {
		order.DoneReason = event.Status
	}
	d.topic.SendCh() <- order
	d.serverTime = serverTime
}
