// Copyright (c) 2025 BVK Chaitanya

package internal

import (
	"context"
	"encoding/json"
	"testing"
)

func TestClient(t *testing.T) {
	ctx := context.Background()

	opts := &Options{}
	c, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := c.Close(); err != nil {
			t.Fatal(err)
		}
	}()

	mstatus, err := c.GetMarketStatus(ctx, "BTCUSDT")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%#v", mstatus)

	jsdata, _ := json.MarshalIndent(mstatus, "", "  ")
	t.Logf("%s", jsdata)
}
