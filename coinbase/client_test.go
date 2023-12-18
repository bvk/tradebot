// Copyright (c) 2023 BVK Chaitanya

package coinbase

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/bvkgo/kv/kvmemdb"
)

var (
	testingKey    string
	testingSecret string
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

	ctx := context.Background()
	exch, err := New(ctx, kvmemdb.New(), testingKey, testingSecret, SubcommandOptions())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := exch.Close(); err != nil {
			t.Fatal(err)
		}
	}()
}
