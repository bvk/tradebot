// Copyright (c) 2025 BVK Chaitanya

package coinex

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bvk/tradebot/coinex/internal"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

var (
	testingKey    string
	testingSecret string
)

func checkCredentials() bool {
	if len(testingKey) != 0 && len(testingSecret) != 0 {
		return true
	}
	data, err := os.ReadFile("coinex-creds.json")
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

	markets, err := c.GetMarkets(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%#v", markets)

	minfo, err := c.GetMarketInfo(ctx, "BCHUSDT")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%#v", minfo)

	balances, err := c.GetBalances(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%#v", balances)

	for order := range c.ListFilledOrders(ctx, "", "", &err) {
		cat := time.UnixMilli(order.CreatedAtMilli)
		uat := time.UnixMilli(order.UpdatedAtMilli)
		t.Logf("create=%s market=%s side=%s size=%v price=%v finish=%s", cat.Format(time.RFC3339), order.Market, order.Side, order.FilledAmount, order.OrderPrice, uat.Format(time.RFC3339))
	}
	if err != nil {
		t.Fatal(err)
	}

	jsdata, _ := json.MarshalIndent(balances, "", "  ")
	t.Logf("%s", jsdata)
}

func TestJSONRawMessage(t *testing.T) {
	v := `{
    "id": 1,
    "code": 0,
    "data": {
        "result": "pong"
    },
    "message": "OK"
}`
	var msg json.RawMessage
	if err := json.Unmarshal([]byte(v), &msg); err != nil {
		t.Fatal(err)
	}
	header := new(internal.WebsocketHeader)
	if err := json.Unmarshal([]byte(msg), header); err != nil {
		t.Fatal(err)
	}
	t.Logf("%#v", header)

	response := new(internal.WebsocketResponse)
	if err := json.Unmarshal([]byte(msg), response); err != nil {
		t.Fatal(err)
	}
	t.Logf("%#v", response)

	jsdata, _ := json.MarshalIndent(response, "", "  ")
	t.Logf("%s", jsdata)
}

func TestWebsocket(t *testing.T) {
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

	if err := c.websocketPing(ctx); err != nil {
		t.Fatal(err)
	}

	stime, err := c.websocketServerTime(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("server-time: %v", stime)

	if err := c.websocketSign(ctx); err != nil {
		t.Fatal(err)
	}

	// Check for thread-safey in websocket-calls.
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()

			if i%2 == 0 {
				if err := c.websocketPing(ctx); err != nil {
					t.Fatal(err)
				}
			} else {
				if _, err := c.websocketServerTime(ctx); err != nil {
					t.Fatal(err)
				}
			}
		}(i)
	}
	wg.Wait()

	// Subscribe for market price updates.
	if err := c.WatchMarket(ctx, "DOGEUSDT"); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := c.UnwatchMarket(ctx, "DOGEUSDT"); err != nil {
			t.Error(err)
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

	price := minfo.LastPrice.Mul(decimal.NewFromFloat(0.09))
	size := mstatus.MinAmount

	// TODO: Get funds and verify that we can place this order.

	createReq := &internal.CreateOrderRequest{
		ClientOrderID: strings.ReplaceAll(uuid.New().String(), "-", ""),
		Market:        "DOGEUSDT",
		MarketType:    "SPOT",
		Side:          "buy",
		OrderType:     "limit",
		Amount:        size,
		Price:         price,
	}
	createResp, err := c.CreateOrder(ctx, createReq)
	if err != nil {
		t.Fatal(err)
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

	getResp, err := c.GetOrder(ctx, createReq.Market, createResp.OrderID)
	if err != nil {
		t.Error(err)
		return
	}
	t.Logf("get-order-response: %#v", getResp)

	jsdata, _ := json.MarshalIndent(getResp, "", "  ")
	t.Logf("%s", jsdata)

	time.Sleep(5 * time.Second)
}
