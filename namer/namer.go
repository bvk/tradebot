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

func checkEqual(a, b *gobs.NameData) error {
	if a.Name != b.Name {
		return fmt.Errorf("Name field value is not the same")
	}
	if a.ID != b.ID {
		return fmt.Errorf("ID field value is not the same")
	}
	if a.Typename != b.Typename {
		return fmt.Errorf("Typename field value is not the same")
	}
	return nil
}

// ResolveDB is similar to Resolve, but takes a kv.Database argument.
func ResolveDB(ctx context.Context, db kv.Database, str string) (name, id, typename string, err error) {
	resolve := func(ctx context.Context, r kv.Reader) error {
		name, id, typename, err = Resolve(ctx, r, str)
		return nil
	}
	kv.WithReader(ctx, db, resolve)
	return name, id, typename, err
}

// Resolve converts string argument which can be name or id with the namer database.
func Resolve(ctx context.Context, r kv.Reader, str string) (name, id, typename string, err error) {
	if len(str) == 0 {
		return "", "", "", fmt.Errorf("name/id string argument cannot be empty")
	}
	skey := path.Join(Keyspace, toUUID(str))
	data, err := kvutil.Get[gobs.NameData](ctx, r, skey)
	if err != nil {
		return "", "", "", fmt.Errorf("could not fetch naming data: %w", err)
	}
	// Check that other link also points to the same data.
	other := ""
	if data.Name == str {
		other = data.ID
	} else if data.ID == str {
		other = data.Name
	} else {
		return "", "", "", fmt.Errorf("unexpected: name data is inconsistent for %q", str)
	}
	okey := path.Join(Keyspace, toUUID(other))
	if v, err := kvutil.Get[gobs.NameData](ctx, r, okey); err != nil {
		return "", "", "", fmt.Errorf("could not read name data for tag %q: %w", other, err)
	} else if err := checkEqual(data, v); err != nil {
		return "", "", "", fmt.Errorf("unexpected: name data at ID and Name is not the same: %w", err)
	}
	return data.Name, data.ID, data.Typename, nil
}

func SetName(ctx context.Context, rw kv.ReadWriter, name, id, typename string) error {
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
	_, id, _, err := Resolve(ctx, rw, name)
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
	name, _, _, err := Resolve(ctx, rw, id)
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
