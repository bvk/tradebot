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
	"github.com/shopspring/decimal"
)

func (s *Server) doExchangeGetOrder(ctx context.Context, req *api.ExchangeGetOrderRequest) (*api.ExchangeGetOrderResponse, error) {
	if err := req.Check(); err != nil {
		return nil, err
	}
	account, ok := s.accountMap[strings.ToLower(req.AccountName)]
	if !ok {
		return nil, fmt.Errorf("no account with name %q: %w", req.AccountName, os.ErrNotExist)
	}
	order, err := account.GetOrder(ctx, req.ProductID, req.OrderID)
	if err != nil {
		return &api.ExchangeGetOrderResponse{Error: err.Error()}, nil
	}
	var price decimal.Decimal
	if !order.ExecutedSize().IsZero() {
		price = order.ExecutedValue().Div(order.ExecutedSize())
	}
	resp := &api.ExchangeGetOrderResponse{
		Order: &gobs.Order{
			ServerOrderID: order.ServerID(),
			ClientOrderID: order.ClientID().String(),
			Side:          order.OrderSide(),
			Status:        order.OrderStatus(),
			CreateTime:    order.CreatedAt(),
			FinishTime:    order.FinishedAt(),
			FilledFee:     order.ExecutedFee(),
			FilledSize:    order.ExecutedSize(),
			FilledPrice:   price,
			Done:          order.IsDone(),
			DoneReason:    order.OrderStatus(),
		},
	}
	return resp, nil
}

func (s *Server) doGetProduct(ctx context.Context, req *api.ExchangeGetProductRequest) (*api.ExchangeGetProductResponse, error) {
	accountName := strings.ToLower(req.AccountName)
	account, ok := s.accountMap[accountName]
	if !ok {
		return nil, fmt.Errorf("no account with name %q: %w", req.AccountName, os.ErrNotExist)
	}
	product, err := account.GetSpotProduct(ctx, req.Base, req.Quote)
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
	if _, ok := s.exchangeMap[exchangeName]; !ok {
		return nil, fmt.Errorf("no exchange with name %q: %w", req.ExchangeName, os.ErrNotExist)
	}
	var account exchange.Account
	for _, a := range s.accountMap {
		if a.ExchangeName() == exchangeName {
			account = a
			break
		}
	}
	if account == nil {
		return nil, fmt.Errorf("exchange %q has no accounts", req.ExchangeName)
	}
	p, err := account.GetSpotProduct(ctx, req.Base, req.Quote)
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

	if err := kvutil.SetDB[gobs.ServerState](ctx, s.db, ServerStateKey, state); err != nil {
		return nil, err
	}

	s.state = state
	resp := &api.ExchangeUpdateProductResponse{
		Product: p,
	}
	return resp, nil
}
