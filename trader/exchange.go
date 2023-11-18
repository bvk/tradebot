// Copyright (c) 2023 BVK Chaitanya

package trader

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/bvk/tradebot/api"
	"github.com/bvk/tradebot/exchange"
	"github.com/bvk/tradebot/gobs"
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
		Order: &gobs.Order{
			ServerOrderID: string(order.OrderID),
			ClientOrderID: order.ClientOrderID,
			Side:          order.Side,
			Status:        order.Status,
			CreateTime:    gobs.RemoteTime{Time: order.CreateTime.Time},
			FilledFee:     order.Fee,
			FilledSize:    order.FilledSize,
			FilledPrice:   order.FilledPrice,
			Done:          order.Done,
			DoneReason:    order.DoneReason,
		},
	}
	return resp, nil
}

func (t *Trader) doGetProduct(ctx context.Context, req *api.ExchangeGetProductRequest) (*api.ExchangeGetProductResponse, error) {
	ex, ok := t.exchangeMap[strings.ToLower(req.ExchangeName)]
	if !ok {
		return nil, fmt.Errorf("no exchange with name %q: %w", req.ExchangeName, os.ErrNotExist)
	}
	product, err := ex.GetProduct(ctx, req.ProductID)
	if err != nil {
		return &api.ExchangeGetProductResponse{Error: err.Error()}, nil
	}
	return &api.ExchangeGetProductResponse{Product: product}, nil
}

func (t *Trader) doGetCandles(ctx context.Context, req *api.ExchangeGetCandlesRequest) (*api.ExchangeGetCandlesResponse, error) {
	ex, ok := t.exchangeMap[strings.ToLower(req.ExchangeName)]
	if !ok {
		return nil, fmt.Errorf("no exchange with name %q: %w", req.ExchangeName, os.ErrNotExist)
	}
	candles, err := ex.GetCandles(ctx, req.ProductID, req.StartTime)
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
		last := candles[len(candles)-1]
		resp.Continue.StartTime = last.StartTime.Time.Add(last.Duration)
	}
	return resp, nil
}
