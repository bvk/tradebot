// Copyright (c) 2026 BVK Chaitanya

package server

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/bvk/tradebot/api"
	"github.com/bvk/tradebot/setup"
	"github.com/bvkgo/kv/kvmemdb"
)

func TestSetupHandler(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()

	db := kvmemdb.New()
	secretsPath := filepath.Join(tmpDir, "secrets.json")
	opts := &Options{
		DataDir: tmpDir,
	}

	s, err := New(ctx, secretsPath, db, opts)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := s.Close(); err != nil {
			t.Fatal(err)
		}
	}()

	req1 := new(api.SetupRequest)
	resp1, err := s.doSetup(ctx, req1)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%#v", resp1)

	req2 := &api.SetupRequest{
		Pushover: &setup.Pushover{
			UserKey:        "user-key",
			ApplicationKey: "application-key",
		},
	}
	resp2, err := s.doSetup(ctx, req2)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%#v", resp2)

	secrets1, err := SecretsFromFile(secretsPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%v", secrets1)
}
