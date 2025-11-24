// Copyright (c) 2025 BVK Chaitanya

package coinbase

import (
	"context"
	"testing"
)

func TestGetProductPriceMap(t *testing.T) {
	ctx := context.Background()
	pmap, err := GetProductPriceMap(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Log(pmap)
}

func TestGetPriceMap(t *testing.T) {
	ctx := context.Background()
	prodPriceMap, err := GetProductPriceMap(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Log(GetPriceMap(prodPriceMap))
}
