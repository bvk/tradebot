// Copyright (c) 2023 BVK Chaitanya

package idgen

import (
	"crypto/md5"
	"encoding/binary"
	"sync"

	"github.com/google/uuid"
)

// Generator creates sequence of uuids derived from a given base uuid.
type Generator struct {
	mu sync.Mutex

	seed string
	base uuid.UUID

	next  uint64
	cache []uuid.UUID
}

func New(seed string, offset uint64) *Generator {
	base := uuid.UUID(md5.Sum([]byte(seed)))
	return &Generator{seed: seed, base: base, next: offset}
}

func (v *Generator) Seed() string {
	return v.seed
}

func (v *Generator) Offset() uint64 {
	v.mu.Lock()
	defer v.mu.Unlock()

	return v.next
}

func (v *Generator) NextID() uuid.UUID {
	v.mu.Lock()
	defer v.mu.Unlock()

	if len(v.cache) == 0 || v.next%10 == 0 {
		v.cache = v.prepare(v.next/10*10, 10)
	}
	id := v.cache[v.next%10]
	v.next++
	return id
}

func (v *Generator) RevertID() {
	v.mu.Lock()
	defer v.mu.Unlock()

	if v.next > 0 {
		v.next--
		v.cache = nil
	}
}

func (v *Generator) prepare(from, n uint64) []uuid.UUID {
	var buf [16 + 8]byte
	copy(buf[:16], []byte(v.base[:]))

	ids := make([]uuid.UUID, 0, n)
	for i := uint64(0); i < n; i++ {
		binary.BigEndian.PutUint64(buf[16:], from+i)
		checksum := md5.Sum(buf[:])
		ids = append(ids, uuid.UUID(checksum))
	}
	return ids
}
