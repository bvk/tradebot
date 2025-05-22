// Copyright (c) 2025 BVK Chaitanya

package internal

import (
	"context"
	"encoding/json"
	"os"
	"sync"
	"testing"
	"time"
)

var (
	testingKey    string
	testingSecret string
)

func checkCredentials() bool {
	type Credentials struct {
		Key    string
		Secret string
	}
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
	c, err := New(testingKey, testingSecret, opts)
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
	header := new(WebsocketHeader)
	if err := json.Unmarshal([]byte(msg), header); err != nil {
		t.Fatal(err)
	}
	t.Logf("%#v", header)

	response := new(WebsocketResponse)
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
	c, err := New(testingKey, testingSecret, opts)
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
	if err := c.WatchMarket(ctx, "BTCUSDT"); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := c.UnwatchMarket(ctx, "BTCUSDT"); err != nil {
			t.Error(err)
		}
	}()

	time.Sleep(time.Second)
}
