// Copyright (c) 2023 BVK Chaitanya

package server

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/bvk/tradebot/api"
	"github.com/bvk/tradebot/exchange"
	"github.com/bvk/tradebot/gobs"
	"github.com/bvk/tradebot/kvutil"
)

func (s *Server) doExchangeGetOrder(ctx context.Context, req *api.ExchangeGetOrderRequest) (*api.ExchangeGetOrderResponse, error) {
	if err := req.Check(); err != nil {
		return nil, err
	}
	ex, ok := s.exchangeMap[strings.ToLower(req.ExchangeName)]
	if !ok {
		return nil, fmt.Errorf("no exchange with name %q: %w", req.ExchangeName, os.ErrNotExist)
	}
	order, err := ex.GetOrder(ctx, req.ProductID, exchange.OrderID(req.OrderID))
	if err != nil {
		return &api.ExchangeGetOrderResponse{Error: err.Error()}, nil
	}
	resp := &api.ExchangeGetOrderResponse{
		Order: &gobs.Order{
			ServerOrderID: string(order.ServerOrderID),
			ClientOrderID: order.ClientID().String(),
			Side:          order.Side,
			Status:        order.Status,
			CreateTime:    gobs.RemoteTime{Time: order.CreateTime.Time},
			FinishTime:    gobs.RemoteTime{Time: order.FinishTime.Time},
			FilledFee:     order.Fee,
			FilledSize:    order.FilledSize,
			FilledPrice:   order.FilledPrice,
			Done:          order.Done,
			DoneReason:    order.DoneReason,
		},
	}
	return resp, nil
}

func (s *Server) doGetProduct(ctx context.Context, req *api.ExchangeGetProductRequest) (*api.ExchangeGetProductResponse, error) {
	exchangeName := strings.ToLower(req.ExchangeName)
	ex, ok := s.exchangeMap[exchangeName]
	if !ok {
		return nil, fmt.Errorf("no exchange with name %q: %w", req.ExchangeName, os.ErrNotExist)
	}
	product, err := ex.GetSpotProduct(ctx, req.Base, req.Quote)
	if err != nil {
		return &api.ExchangeGetProductResponse{Error: err.Error()}, nil
	}
	return &api.ExchangeGetProductResponse{Product: product}, nil
}

func (s *Server) doExchangeUpdateProduct(ctx context.Context, req *api.ExchangeUpdateProductRequest) (*api.ExchangeUpdateProductResponse, error) {
	if err := req.Check(); err != nil {
		return nil, err
	}

	usds := []string{"USD", "USDT", "USDC"}
	if !slices.Contains(usds, req.Quote) {
		return nil, fmt.Errorf("quote must be one of %#v", usds)
	}

	exchangeName := strings.ToLower(req.ExchangeName)
	ex, ok := s.exchangeMap[exchangeName]
	if !ok {
		return nil, fmt.Errorf("no exchange with name %q: %w", req.ExchangeName, os.ErrNotExist)
	}
	p, err := ex.GetSpotProduct(ctx, req.Base, req.Quote)
	if err != nil {
		return nil, fmt.Errorf("could not find spot product for base=%s quote=%s: %w", req.Base, req.Quote, err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	state, err := gobs.Clone(s.state)
	if err != nil {
		return nil, fmt.Errorf("could not clone gobs.ServerState: %w", err)
	}

	estate, ok := state.ExchangeMap[exchangeName]
	if !ok {
		estate = new(gobs.ServerExchangeState)
		state.ExchangeMap[exchangeName] = estate
	}

	if req.Enable {
		if slices.Contains(estate.EnabledProductIDs, p.ProductID) {
			return nil, os.ErrExist
		}
		estate.EnabledProductIDs = append(estate.EnabledProductIDs, p.ProductID)
	} else {
		index := slices.Index(estate.EnabledProductIDs, p.ProductID)
		if index == -1 {
			return nil, os.ErrNotExist
		}
		estate.EnabledProductIDs = slices.Delete(estate.EnabledProductIDs, index, index+1)
	}
	// TODO: Handle watch/unwatch mode.

	if err := kvutil.SetDB[gobs.ServerState](ctx, s.db, serverStateKey, state); err != nil {
		return nil, err
	}

	s.state = state
	resp := &api.ExchangeUpdateProductResponse{
		Product: p,
	}
	return resp, nil
}
