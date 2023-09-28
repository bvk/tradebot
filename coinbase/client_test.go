// Copyright (c) 2023 BVK Chaitanya

package coinbase

import (
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

var (
	testingKey     string   = "Cki0qSa9C6O0GI98"
	testingSecret  string   = "0m3C3trHZSxe9ayFzoldSIvIlgNr7HLS"
	testingOptions *Options = &Options{}
)

func checkCredentials() bool {
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

	order, err := c.getOrder(c.ctx, orders[0].ServerOrderID)
	if err != nil {
		t.Fatal(err)
	}

	testPrice, _ := decimal.NewFromString("0.01")
	testSize, _ := decimal.NewFromString("1")
	testID := uuid.New()
	testOrder, err := bch.Buy(c.ctx, testID.String(), testSize, testPrice)
	if err != nil {
		t.Fatal(err)
	}

	if err := bch.Cancel(c.ctx, testOrder.ServerOrderID); err != nil {
		t.Fatal(err)
	}

	t.Logf("BCH price is at %s", bch.Price())
	t.Logf("Found a live order %#v", order)
}
