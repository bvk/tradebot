// Copyright (c) 2023 BVK Chaitanya

package coinbase

import (
	"encoding/json"
	"os"
	"slices"
	"testing"
)

var (
	testingKey     string
	testingSecret  string
	testingOptions *Options = &Options{}
)

func checkCredentials() bool {
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

	c, err := New(testingKey, testingSecret, testingOptions)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := c.Close(); err != nil {
			t.Fatal(err)
		}
	}()

	if !slices.Contains(c.spotProducts, "BCH-USD") {
		t.Skipf("product list has no BCH-USD product")
		return
	}

	bch, err := c.NewProduct(c.ctx, "BCH-USD")
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseProduct(bch)

	orders, err := bch.List(c.ctx)
	if err != nil {
		t.Fatal(err)
	}

	if len(orders) == 0 {
		t.Skipf("no live orders to test further")
		return
	}
}
