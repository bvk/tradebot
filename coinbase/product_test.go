// Copyright (c) 2023 BVK Chaitanya

package coinbase

import (
	"encoding/json"
	"testing"
)

func TestProduct(t *testing.T) {
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

	p, err := c.NewProduct(c.ctx, "BCH-USD")
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseProduct(p)

	jsdata, _ := json.MarshalIndent(p.productData, "", "  ")
	t.Logf("%s\n", jsdata)
}
