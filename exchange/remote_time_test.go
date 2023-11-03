// Copyright (c) 2023 BVK Chaitanya

package exchange

import (
	"bytes"
	"encoding/gob"
	"testing"
	"time"
)

func TestRemoteTimeGob(t *testing.T) {
	type GobType struct {
		Timepoint RemoteTime
	}

	// Check zero timepoint is encoded and decoded correctly.
	var zero GobType
	var zbuf bytes.Buffer
	if err := gob.NewEncoder(&zbuf).Encode(&zero); err != nil {
		t.Fatal(err)
	}
	zrecovered := new(GobType)
	if err := gob.NewDecoder(&zbuf).Decode(zrecovered); err != nil {
		t.Fatal(err)
	}
	if !zrecovered.Timepoint.Time.IsZero() {
		t.Fatalf("IsZero: want true, got false")
	}

	// Check non-zero timepoint is encoded and decoded correctly.
	v := GobType{Timepoint: RemoteTime{Time: time.Now()}}
	var vbuf bytes.Buffer
	if err := gob.NewEncoder(&vbuf).Encode(&v); err != nil {
		t.Fatal(err)
	}
	vrecovered := new(GobType)
	if err := gob.NewDecoder(&vbuf).Decode(vrecovered); err != nil {
		t.Fatal(err)
	}
	if !vrecovered.Timepoint.Equal(v.Timepoint.Time) {
		t.Fatalf("Equal: want true, got false")
	}
}
