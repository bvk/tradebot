// Copyright (c) 2025 BVK Chaitanya

package telegram

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/bvkgo/kv/kvmemdb"
)

var testingSecrets *Secrets

func checkSecrets() bool {
	if testingSecrets != nil {
		return true
	}
	data, err := os.ReadFile("telegram-creds.json")
	if err != nil {
		return false
	}
	s := new(Secrets)
	if err := json.Unmarshal(data, s); err != nil {
		return false
	}
	if err := s.Check(); err != nil {
		return false
	}
	testingSecrets = s
	return true
}

func TestClient(t *testing.T) {
	ctx := context.Background()

	if !checkSecrets() {
		t.Skip("no credentials")
		return
	}

	db := kvmemdb.New()
	c, err := New(ctx, db, testingSecrets)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := c.Close(); err != nil {
			t.Fatal(err)
		}
	}()

	t.Logf("Authorized on account %s with owner %s", c.BotUserName(), c.OwnerUserName())

	c.SendMessage(ctx, time.Now(), "hello")
}
