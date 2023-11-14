// Copyright (c) 2023 BVK Chaitanya

package trader

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

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

func (t *Trader) doGetCandles(ctx context.Context, req *api.ExchangeGetCandlesRequest) (*api.ExchangeGetCandlesResponse, error) {
	ex, ok := t.exchangeMap[strings.ToLower(req.ExchangeName)]
	if !ok {
		return nil, fmt.Errorf("no exchange with name %q: %w", req.ExchangeName, os.ErrNotExist)
	}
	// Coinbase is not returning the candle with start time exactly equal to the
	// req.StartTime, so we adjust startTime by a second.
	candles, err := ex.GetHourCandles(ctx, req.ProductID, req.StartTime.Add(-time.Second))
	if err != nil {
		resp := &api.ExchangeGetCandlesResponse{
			Error: err.Error(),
		}
		return resp, nil
	}
	sort.Slice(candles, func(i, j int) bool {
		return candles[i].StartTime.Time.Before(candles[j].StartTime.Time)
	})
	resp := &api.ExchangeGetCandlesResponse{
		Candles: candles,
	}
	if len(candles) > 1 {
		resp.Continue = req
		resp.Continue.StartTime = candles[len(candles)-1].EndTime.Time.Add(-time.Second)
	}
	return resp, nil
}
