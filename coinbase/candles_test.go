// Copyright (c) 2023 BVK Chaitanya

package coinbase

import (
	"encoding/json"
	"testing"
	"time"
)

func TestProductCandles(t *testing.T) {
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

	from := time.Now().Add(-24 * time.Hour)
	resp, err := c.getProductCandles(c.ctx, "BCH-USD", from, OneMinuteCandle)
	if err != nil {
		t.Fatal(err)
	}

	jsdata, _ := json.MarshalIndent(resp, "", "  ")
	t.Logf("%s\n", jsdata)
}
