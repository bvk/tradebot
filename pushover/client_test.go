// Copyright (c) 2023 BVK Chaitanya

package pushover

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"
)

var testingKeys *Keys

func checkKeys() bool {
	if testingKeys != nil {
		return true
	}
	data, err := os.ReadFile("pushover-keys.json")
	if err != nil {
		return false
	}
	s := new(Keys)
	if err := json.Unmarshal(data, s); err != nil {
		return false
	}
	testingKeys = s
	return true
}

func TestSendMessage(t *testing.T) {
	if !checkKeys() {
		t.Skip("no keys")
		return
	}

	c, err := New(testingKeys)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.SendMessage(context.Background(), time.Now(), t.Name()); err != nil {
		t.Fatal(err)
	}
}
