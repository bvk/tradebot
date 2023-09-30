// Copyright (c) 2023 BVK Chaitanya

package server

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

	t.Logf("started server at %s:%d", s.opts.ListenIP, s.opts.ListenPort)
}
