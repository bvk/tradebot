// Copyright (c) 2023 BVK Chaitanya

package coinbase

import (
	"encoding/json"
	"io/ioutil"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
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
	data, err := ioutil.ReadFile("coinbase-creds.json")
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

	bch, err := c.NewProduct("BCH-USD")
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

	order, err := c.getOrder(c.ctx, string(orders[0].OrderID))
	if err != nil {
		t.Fatal(err)
	}

	testPrice, _ := decimal.NewFromString("0.01")
	testSize, _ := decimal.NewFromString("1")
	testID := uuid.New()
	testOrder, err := bch.LimitBuy(c.ctx, testID.String(), testSize, testPrice)
	if err != nil {
		t.Fatal(err)
	}

	for {
		status, at, ok := c.orderStatus(string(testOrder))
		if !ok {
			t.Fatalf("order just created has no status")
		}
		t.Logf("order %s status is %s", testOrder, status)
		if status == "OPEN" {
			break
		}
		if err := c.waitForStatusChange(c.ctx, string(testOrder), at); err != nil {
			t.Fatal(err)
		}
	}

	if err := bch.Cancel(c.ctx, testOrder); err != nil {
		t.Fatal(err)
	}

	t.Logf("BCH price is at %s", bch.Price())
	t.Logf("Found a live order %#v", order)
}
