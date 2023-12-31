// Copyright (c) 2023 BVK Chaitanya

package httputil

import "testing"

func TestServer(t *testing.T) {
	s, err := New(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := s.Close(); err != nil {
			t.Fatal(err)
		}
	}()

}
