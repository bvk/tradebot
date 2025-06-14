// Copyright (c) 2025 BVK Chaitanya

package coinex

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/bvk/tradebot/coinex/internal"
	"github.com/bvk/tradebot/exchange"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

func TestRPCRetry(t *testing.T) {
	if !checkCredentials() {
		t.Skip("no credentials")
		return
	}

	ctx := context.Background()

	opts := &Options{}
	c, err := New(ctx, testingKey, testingSecret, opts)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := c.Close(); err != nil {
			t.Fatal(err)
		}
	}()

	mstatus, err := c.GetMarket(ctx, "DOGEUSDT")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%#v", mstatus)

	minfo, err := c.GetMarketInfo(ctx, "DOGEUSDT")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%#v", minfo)

	balances, err := c.GetBalances(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%#v", balances)

	price := minfo.LastPrice.Mul(decimal.NewFromFloat(0.09))
	size := mstatus.MinAmount

	clientOrderID := uuid.New().String()
	cleanClientID := strings.ReplaceAll(clientOrderID, "-", "")

	createReq := &internal.CreateOrderRequest{
		ClientOrderID: cleanClientID,
		Market:        "DOGEUSDT",
		MarketType:    "SPOT",
		Side:          "buy",
		OrderType:     "limit",
		Amount:        size,
		Price:         price,
	}
	createResp, err := c.CreateOrder(ctx, createReq)
	if err != nil {
		if !errors.Is(err, exchange.ErrNoFund) {
			t.Fatal(err)
		}
		t.Skip("no funds for the test")
		return
	}
	t.Logf("%#v", createResp)
	defer func() {
		cancelResp, err := c.CancelOrder(ctx, createReq.Market, createResp.OrderID)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("cancel-response: %#v", cancelResp)

		jsdata, _ := json.MarshalIndent(cancelResp, "", "  ")
		t.Logf("%s", jsdata)
	}()

	// Retry must fail with an error.
	createResp2, err := c.CreateOrder(ctx, createReq)
	if err == nil {
		if createResp.OrderID != createResp2.OrderID {
			t.Errorf("retry with same client id created a new order (first=%d != second=%d)", createResp.OrderID, createResp2.OrderID)
		}

		cancelResp, err := c.CancelOrder(ctx, createReq.Market, createResp2.OrderID)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("cancel-response: %#v", cancelResp)
	}
}

func TestProductRPCRetry(t *testing.T) {
	if !checkCredentials() {
		t.Skip("no credentials")
		return
	}

	ctx := context.Background()

	opts := &Options{}
	c, err := New(ctx, testingKey, testingSecret, opts)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := c.Close(); err != nil {
			t.Fatal(err)
		}
	}()

	p, err := NewProduct(ctx, c, "DOGEUSDT")
	if err != nil {
		t.Fatal(err)
	}

	price := p.minfo.LastPrice.Mul(decimal.NewFromFloat(0.09))
	size := p.mstatus.MinAmount

	clientOrderID := uuid.New()
	order, err := p.LimitBuy(ctx, clientOrderID, size, price)
	if err != nil {
		if !errors.Is(err, exchange.ErrNoFund) {
			t.Fatal(err)
		}
		t.Skip("no funds for the test")
		return
	}
	defer func() {
		if err := p.Cancel(ctx, order.ServerID()); err != nil {
			t.Fatal(err)
		}
	}()

	// Retry must fail with an error.
	order2, err := p.LimitBuy(ctx, clientOrderID, size, price)
	if err != nil {
		t.Fatal(err)
	}
	if order.ServerID() != order2.ServerID() {
		t.Errorf("retry with same client id created a new order (first=%s != second=%s)", order.ServerID(), order2.ServerID())
	}
}
