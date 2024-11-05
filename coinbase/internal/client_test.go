// Copyright (c) 2023 BVK Chaitanya

package internal

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/bvk/tradebot/exchange"
	"github.com/bvkgo/topic"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

var (
	testingKey     string
	testingSecret  string
	testingOptions *Options = &Options{}
)

func checkCredentials() bool {
	type Credentials struct {
		Key    string
		Secret string
	}
	if len(testingKey) != 0 && len(testingSecret) != 0 {
		return true
	}
	data, err := os.ReadFile("coinbase-creds.json")
	if err != nil {
		return false
	}
	s := new(Credentials)
	if err := json.Unmarshal(data, s); err != nil {
		return false
	}
	testingKey = s.Key
	testingSecret = s.Secret
	return len(testingKey) != 0 && len(testingSecret) != 0
}

func TestClient(t *testing.T) {
	if !checkCredentials() {
		t.Skip("no credentials")
		return
	}

	topic := topic.New[*Message]()
	defer topic.Close()

	c, err := New(context.Background(), testingKey, testingSecret, testingOptions)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := c.Close(); err != nil {
			t.Fatal(err)
		}
	}()

	ctx := context.Background()
	if _, err := c.ListProducts(ctx, "SPOT"); err != nil {
		t.Fatal(err)
	}

	if _, _, err := c.ListAccounts(ctx, nil); err != nil {
		t.Fatal(err)
	}

	handler := func(msg *Message) {
		if msg.Channel != "heartbeats" {
			js, _ := json.MarshalIndent(msg, "", "  ")
			t.Logf("%s", js)
		}
	}

	products := []string{"DOGE-USDC"}
	ws := c.GetMessages("heartbeats", products, handler)
	ws.Subscribe("user", products)
	ws.Subscribe("ticker", products)

	time.Sleep(30 * time.Second)

	createReq := &CreateOrderRequest{
		ClientOrderID: uuid.New().String(),
		ProductID:     "DOGE-USDC",
		Side:          "SELL",
		Order: &OrderConfig{
			LimitGTC: &LimitLimitGTC{
				BaseSize:   exchange.NullDecimal{Decimal: decimal.NewFromInt(1000)},
				LimitPrice: exchange.NullDecimal{Decimal: decimal.NewFromFloat(0.21)},
				PostOnly:   true,
			},
		},
	}
	createResp, err := c.CreateOrder(ctx, createReq)
	if err != nil {
		t.Fatal(err)
	} else {
		js, _ := json.MarshalIndent(createResp, "", "  ")
		t.Logf("%s", js)
	}
	defer func() {
		cancelReq := &CancelOrderRequest{
			OrderIDs: []string{createResp.SuccessResponse.OrderID},
		}
		if _, err := c.CancelOrder(ctx, cancelReq); err != nil {
			t.Fatal(err)
		}
	}()

	time.Sleep(30 * time.Second)

}
