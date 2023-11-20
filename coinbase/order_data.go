// Copyright (c) 2023 BVK Chaitanya

package coinbase

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"slices"
	"time"

	"github.com/bvk/tradebot/exchange"
	"github.com/bvkgo/topic"
)

var doneStatuses []string = []string{
	"FILLED", "CANCELLED", "EXPIRED", "FAILED",
}

type orderData struct {
	serverTime time.Time

	orderID string

	client *Client

	topic *topic.Topic[*exchange.Order]

	ch <-chan *exchange.Order
}

func (c *Client) newOrderData(id string) *orderData {
	d := &orderData{
		client:  c,
		orderID: id,
		topic:   topic.New[*exchange.Order](),
	}
	_, d.ch, _ = d.topic.Subscribe(0, false /* includeRecent */)
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

	r, ch, _ := d.topic.Subscribe(1, true /* includeRecent */)
	defer r.Unsubscribe()

	var v *exchange.Order
	for i := 1; v == nil || !slices.Contains(wanted, v.Status); i++ {
		select {
		case <-ctx.Done():
			return "", context.Cause(ctx)
		case <-time.After(time.Duration(i) * time.Second):
			resp, err := d.client.getOrder(ctx, d.orderID)
			if err != nil {
				log.Printf("could not poll for order %q after timeout (will retry): %v", d.orderID, err)
				continue
			}
			d.topic.SendCh() <- toExchangeOrder(&resp.Order)
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
	d.topic.SendCh() <- order
	d.serverTime = serverTime
}
