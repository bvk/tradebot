// Copyright (c) 2023 BVK Chaitanya

package ctxutil

import (
	"context"
	"testing"
)

func TestCloseGroup(t *testing.T) {
	var cg CloseGroup

	for i := 0; i < 100; i++ {
		i := i
		cg.Go(func(ctx context.Context) {
			<-ctx.Done()
			t.Logf("%d complete", i)
		})
	}

	cg.Close()
	t.Logf("DONE")
}
