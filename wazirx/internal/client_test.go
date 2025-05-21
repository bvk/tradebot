// Copyright (c) 2025 BVK Chaitanya

package internal

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"testing"
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
	data, err := os.ReadFile("wazirx-creds.json")
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

	// Set custom logging backend to capture debug messages if necessary.
	{
		logLevel := new(slog.LevelVar)
		logLevel.Set(slog.LevelInfo)

		slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
			Level: logLevel,
		})))

		logLevel.Set(slog.LevelDebug)
	}

	ctx := context.Background()
	c, err := New(ctx, testingKey, testingSecret, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := c.Close(); err != nil {
			t.Fatal(err)
		}
	}()

	exinfo, err := c.GetExchangeInfo(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%#v", exinfo)

	funds, err := c.GetFunds(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%#v", funds)

	js, _ := json.MarshalIndent(funds, "", "  ")
	t.Logf("%s", js)
}
