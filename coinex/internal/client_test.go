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

	jsdata, _ := json.MarshalIndent(minfo, "", "  ")
	t.Logf("%s", jsdata)
}
