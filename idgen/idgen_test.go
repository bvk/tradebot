// Copyright (c) 2023 BVK Chaitanya

package idgen

import (
	"math/rand"
	"testing"

	"github.com/google/uuid"
)

func TestIDGen(t *testing.T) {
	uid := "unique message id"

	g1 := New(uid, 0)
	g1ids := make(map[int]uuid.UUID)
	for i := 0; i < 20; i++ {
		g1ids[i] = g1.NextID()
	}

	g2 := New(uid, 1)
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

	g1 := New(uid, 0)
	offset := rand.Intn(20)
	for i := 0; i < offset; i++ {
		g1.NextID()
	}

	g2 := New(uid, g1.Offset())
	if a, b := g1.NextID(), g2.NextID(); a != b {
		t.Fatalf("want %v, got %v", a, b)
	}
}

func TestIDGenRevert(t *testing.T) {
	g1 := New(t.Name(), 0)
	idMap := make(map[uint64]uuid.UUID)
	for i := 0; i < 100; i++ {
		idMap[g1.Offset()] = g1.NextID()
	}

	g2 := New(t.Name(), 0)
	for i := 0; i < rand.Intn(20); i++ {
		g2.NextID()
	}

	g2.RevertID()
	wanted := idMap[g2.Offset()]
	if id := g2.NextID(); wanted != id {
		t.Fatalf("want %v, got %v", wanted, id)
	}
}
