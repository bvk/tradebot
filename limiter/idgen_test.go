// Copyright (c) 2023 BVK Chaitanya

package limiter

import (
	"math/rand"
	"testing"

	"github.com/google/uuid"
)

func TestIDGen(t *testing.T) {
	uid := "unique message id"

	g1 := newIDGenerator(uid, 0)
	g1ids := make(map[int]uuid.UUID)
	for i := 0; i < 20; i++ {
		g1ids[i] = g1.NextID()
	}

	g2 := newIDGenerator(uid, 1)
	g2ids := make(map[int]uuid.UUID)
	for i := 0; i < 20; i++ {
		g2ids[1+i] = g2.NextID()
	}

	for k, v := range g2ids {
		if x, ok := g1ids[k]; ok && x != v {
			t.Fatalf("want %v, got %v", x, v)
		}
	}
}

func TestIDGenOffset(t *testing.T) {
	uid := "unique id"

	g1 := newIDGenerator(uid, 0)
	offset := rand.Intn(20)
	for i := 0; i < offset; i++ {
		g1.NextID()
	}

	g2 := newIDGenerator(uid, g1.Offset())
	if a, b := g1.NextID(), g2.NextID(); a != b {
		t.Fatalf("want %v, got %v", a, b)
	}
}
