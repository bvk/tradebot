// Copyright (c) 2025 BVK Chaitanya

package internal

import (
	"context"
	"encoding/json"
	"os"
	"testing"
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

	data, err := os.ReadFile("coindcx-creds.json")
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
	c, err := New(ctx, testingKey, testingSecret, testingOptions)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := c.Close(); err != nil {
			t.Fatal(err)
		}
	}()

	markets, err := c.ListMarkets(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("ListMarkets: %#v", markets)

	mdetails, err := c.ListMarketDetails(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("ListMarketDetails: %#v", mdetails)

	balances, err := c.GetBalances(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("GetBalances: %#v", balances)

	uinfos, err := c.GetUserInfo(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("GetUsersInfo: %#v", uinfos)
}
