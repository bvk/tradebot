// Copyright (c) 2023 BVK Chaitanya

package coinbase

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/bvkgo/kv/kvmemdb"
)

func TestProductCandles(t *testing.T) {
	if !checkCredentials() {
		t.Skip("no credentials")
		return
	}

	ctx := context.Background()
	ex, err := New(ctx, kvmemdb.New(), testingKID, testingPEM, SubcommandOptions())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := ex.Close(); err != nil {
			t.Fatal(err)
		}
	}()

	from := time.Now().Add(-24 * time.Hour)
	resp, err := ex.GetCandles(ctx, "BCH-USD", from)
	if err != nil {
		t.Fatal(err)
	}

	jsdata, _ := json.MarshalIndent(resp, "", "  ")
	t.Logf("%s\n", jsdata)
}
