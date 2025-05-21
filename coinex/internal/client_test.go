// Copyright (c) 2025 BVK Chaitanya

package internal

import (
	"context"
	"encoding/json"
	"os"
	"testing"
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
