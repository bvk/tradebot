// Copyright (c) 2023 BVK Chaitanya

package server

import (
	"context"
	"crypto/md5"
	"fmt"
	"path"

	"github.com/bvk/tradebot/dbutil"
	"github.com/bvk/tradebot/gobs"
	"github.com/bvkgo/kv"
	"github.com/google/uuid"
)

func ResolveName(ctx context.Context, db kv.Database, name string) (string, error) {
	checksum := md5.Sum([]byte(name))
	key := path.Join(NamesKeyspace, uuid.UUID(checksum).String())
	old, err := dbutil.Get[gobs.NameData](ctx, db, key)
	if err != nil {
		return "", fmt.Errorf("could not load old name data: %w", err)
	}
	return old.Data, nil
}
