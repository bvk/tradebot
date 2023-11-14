// Copyright (c) 2023 BVK Chaitanya

package trader

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/bvk/tradebot/api"
	"github.com/bvk/tradebot/exchange"
)

func (t *Trader) doExchangeGetOrder(ctx context.Context, req *api.ExchangeGetOrderRequest) (*api.ExchangeGetOrderResponse, error) {
	ex, ok := t.exchangeMap[strings.ToLower(req.Name)]
	if !ok {
		return nil, fmt.Errorf("no exchange with name %q: %w", req.Name, os.ErrNotExist)
	}
	order, err := ex.GetOrder(ctx, exchange.OrderID(req.OrderID))
	if err != nil {
		return &api.ExchangeGetOrderResponse{Error: err.Error()}, nil
	}
	resp := &api.ExchangeGetOrderResponse{
		OrderID:       string(order.OrderID),
		ClientOrderID: order.ClientOrderID,
		Side:          order.Side,
		CreateTime:    order.CreateTime.Time,
		Fee:           order.Fee,
		FilledSize:    order.FilledSize,
		FilledPrice:   order.FilledPrice,
		Status:        order.Status,
		Done:          order.Done,
		DoneReason:    order.DoneReason,
	}
	return resp, nil
}
