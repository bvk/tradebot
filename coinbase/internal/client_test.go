// Copyright (c) 2023 BVK Chaitanya

package internal

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/bvkgo/topic"
)

var (
	testingKey     string
	testingSecret  string
	testingOptions *Options = &Options{}
)

func checkCredentials() bool {
	type Credentials struct {
		Key    string
		Secret string
	}
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

	topic := topic.New[*Message]()
	defer topic.Close()

	c, err := New(context.Background(), testingKey, testingSecret, testingOptions)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := c.Close(); err != nil {
			t.Fatal(err)
		}
	}()

	ctx := context.Background()
	if ps, err := c.ListProducts(ctx, "SPOT"); err != nil {
		t.Fatal(err)
	} else {
		js, _ := json.MarshalIndent(ps, "", "  ")
		t.Logf("%s", js)
	}

	if as, _, err := c.ListAccounts(ctx, nil); err != nil {
		t.Fatal(err)
	} else {
		js, _ := json.MarshalIndent(as, "", "  ")
		t.Logf("%s", js)
	}
}
