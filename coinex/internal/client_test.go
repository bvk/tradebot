// Copyright (c) 2025 BVK Chaitanya

package internal

import (
	"testing"
)

func TestClient(t *testing.T) {
	c, err := New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := c.Close(); err != nil {
			t.Fatal(err)
		}
	}()
}
