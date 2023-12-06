// Copyright (c) 2023 BVK Chaitanya

package namer

import (
	"context"
	"crypto/md5"
	"errors"
	"fmt"
	"os"
	"path"

	"github.com/bvk/tradebot/gobs"
	"github.com/bvk/tradebot/kvutil"
	"github.com/bvkgo/kv"
	"github.com/google/uuid"
)

var Keyspace = "/names/"

func ResolveName(ctx context.Context, r kv.Reader, name string) (id, typename string, err error) {
	if len(name) == 0 {
		return "", "", fmt.Errorf("name cannot be empty")
	}
	nkey := path.Join(Keyspace, toUUID(name))
	data, err := kvutil.Get[gobs.NameData](ctx, r, nkey)
	if err != nil {
		return "", "", fmt.Errorf("could not fetch name-to-id data: %w", err)
	}
	return data.ID, data.Typename, nil
}

func ResolveID(ctx context.Context, r kv.Reader, id string) (name, typename string, err error) {
	if len(id) == 0 {
		return "", "", fmt.Errorf("id cannot be empty")
	}
	ikey := path.Join(Keyspace, toUUID(id))
	data, err := kvutil.Get[gobs.NameData](ctx, r, ikey)
	if err != nil {
		return "", "", fmt.Errorf("could not fetch id-to-name data: %w", err)
	}
	return data.Name, data.Typename, nil
}

func SetName(ctx context.Context, rw kv.ReadWriter, id, name, typename string) error {
	if len(id) == 0 || len(name) == 0 {
		return fmt.Errorf("id and name must be non-empty")
	}
	data := &gobs.NameData{
		ID:       id,
		Name:     name,
		Typename: typename,
	}
	nkey := path.Join(Keyspace, toUUID(name))
	if err := kvutil.Set(ctx, rw, nkey, data); err != nil {
		return fmt.Errorf("could not set name key: %w", err)
	}
	ikey := path.Join(Keyspace, toUUID(id))
	if err := kvutil.Set(ctx, rw, ikey, data); err != nil {
		return fmt.Errorf("could not set id key: %w", err)
	}
	return nil
}

func Rename(ctx context.Context, rw kv.ReadWriter, older, newer string) error {
	if len(older) == 0 || len(newer) == 0 {
		return fmt.Errorf("older and newer names cannot be empty")
	}
	okey := path.Join(Keyspace, toUUID(older))
	data, err := kvutil.Get[gobs.NameData](ctx, rw, okey)
	if err != nil {
		return fmt.Errorf("could not resolve older name: %w", err)
	}
	nkey := path.Join(Keyspace, toUUID(newer))
	if older != newer {
		if _, err := rw.Get(ctx, nkey); err == nil {
			return fmt.Errorf("newer name already exists: %w", os.ErrExist)
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("could not check if newer key already exists: %w", err)
		}
	}
	data.Name = newer
	if err := kvutil.Set(ctx, rw, nkey, data); err != nil {
		return fmt.Errorf("could not set newer key: %w", err)
	}
	ikey := path.Join(Keyspace, toUUID(data.ID))
	if err := kvutil.Set(ctx, rw, ikey, data); err != nil {
		return fmt.Errorf("could not update id key: %w", err)
	}
	if older != newer {
		if err := rw.Delete(ctx, okey); err != nil {
			return fmt.Errorf("could not delete older key: %w", err)
		}
	}
	return nil
}

func Delete(ctx context.Context, rw kv.ReadWriter, name string) error {
	id, _, err := ResolveName(ctx, rw, name)
	if err != nil {
		return fmt.Errorf("could not resolve name %q: %w", name, err)
	}
	nkey := path.Join(Keyspace, toUUID(name))
	if err := rw.Delete(ctx, nkey); err != nil {
		return fmt.Errorf("could not delete name key: %w", err)
	}
	ikey := path.Join(Keyspace, toUUID(id))
	if err := rw.Delete(ctx, ikey); err != nil {
		return fmt.Errorf("could not delete id key: %w", err)
	}
	return nil
}

func DeleteID(ctx context.Context, rw kv.ReadWriter, id string) error {
	name, _, err := ResolveID(ctx, rw, id)
	if err != nil {
		return fmt.Errorf("could not resolve id %q: %w", id, err)
	}
	ikey := path.Join(Keyspace, toUUID(id))
	if err := rw.Delete(ctx, ikey); err != nil {
		return fmt.Errorf("could not delete id key: %w", err)
	}
	nkey := path.Join(Keyspace, toUUID(name))
	if err := rw.Delete(ctx, nkey); err != nil {
		return fmt.Errorf("could not delete name key: %w", err)
	}
	return nil
}

func toUUID(s string) string {
	checksum := md5.Sum([]byte(s))
	return uuid.UUID(checksum).String()
}

func Upgrade(ctx context.Context, rw kv.ReadWriter, name string) error {
	nkey := path.Join(Keyspace, toUUID(name))
	data, err := kvutil.Get[gobs.NameData](ctx, rw, nkey)
	if err != nil {
		return fmt.Errorf("could not fetch name data: %w", err)
	}
	if data.Data == "" {
		return nil
	}
	_, id := path.Split(data.Data)
	if _, err := uuid.Parse(id); err != nil {
		return fmt.Errorf("could not parse uuid from id data: %w", err)
	}
	if data.ID == "" {
		data.ID = id
	}
	if data.Typename == "" {
		data.Typename = "Waller"
	}
	if err := kvutil.Set(ctx, rw, nkey, data); err != nil {
		return fmt.Errorf("could not update name key data: %w", err)
	}
	ikey := path.Join(Keyspace, toUUID(data.ID))
	if err := kvutil.Set(ctx, rw, ikey, data); err != nil {
		return fmt.Errorf("could not update id key data: %w", err)
	}
	return nil
}
