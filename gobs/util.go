// Copyright (c) 2025 BVK Chaitanya

package gobs

import (
	"bytes"
	"encoding/gob"
)

func Clone[PT *T, T any](v PT) (PT, error) {
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(v); err != nil {
		return nil, err
	}
	x := new(T)
	if err := gob.NewDecoder(bytes.NewReader(buf.Bytes())).Decode(x); err != nil {
		return nil, err
	}
	return x, nil
}
