// Copyright (c) 2023 BVK Chaitanya

package kvutil

import (
	"bufio"
	"bytes"
	"context"
	"encoding/gob"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"

	"github.com/bvk/tradebot/gobs"
	"github.com/bvkgo/kv"
)

func Export(ctx context.Context, r kv.Reader, w io.Writer) error {
	it, err := r.Scan(ctx)
	if err != nil {
		return fmt.Errorf("could not create scanning iterator: %w", err)
	}
	defer kv.Close(it)

	encoder := gob.NewEncoder(w)
	for k, v, err := it.Fetch(ctx, false); err == nil; k, v, err = it.Fetch(ctx, true) {
		value, err := io.ReadAll(v)
		if err != nil {
			return fmt.Errorf("could not read value at key %q: %w", k, err)
		}
		item := &gobs.KeyValue{
			Key:   k,
			Value: value,
		}
		if err := encoder.Encode(item); err != nil {
			return fmt.Errorf("could not encode key/value item: %w", err)
		}
	}
	if _, _, err := it.Fetch(ctx, false); err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("iterator fetch has failed: %w", err)
	}
	return nil
}

func Import(ctx context.Context, r io.Reader, rw kv.ReadWriter) error {
	decoder := gob.NewDecoder(r)

	var err error
	var item gobs.KeyValue
	for err = decoder.Decode(&item); err == nil; err = decoder.Decode(&item) {
		if err := rw.Set(ctx, item.Key, bytes.NewReader(item.Value)); err != nil {
			return fmt.Errorf("could not restore at key %q: %w", item.Key, err)
		}
		item = gobs.KeyValue{}
	}

	if !errors.Is(err, io.EOF) {
		return fmt.Errorf("could not decode item from backup file: %w", err)
	}
	return nil
}

func DeleteAll(ctx context.Context, rw kv.ReadWriter) error {
	it, err := rw.Scan(ctx)
	if err != nil {
		return fmt.Errorf("could not create scanning iterator: %w", err)
	}
	defer kv.Close(it)
	for k, _, err := it.Fetch(ctx, false); err == nil; k, _, err = it.Fetch(ctx, true) {
		if err := rw.Delete(ctx, k); err != nil {
			return fmt.Errorf("could not delete key %q: %w", k, err)
		}
	}
	if _, _, err := it.Fetch(ctx, false); err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("iterator fetch has failed: %w", err)
	}
	return nil
}

func BackupDB(ctx context.Context, db kv.Database, file string) (status error) {
	abspath, err := filepath.Abs(file)
	if err != nil {
		return fmt.Errorf("could not determine absolute path: %w", err)
	}

	fp, err := os.CreateTemp(path.Dir(abspath), ".backup*")
	if err != nil {
		return fmt.Errorf("could not create temp file: %w", err)
	}
	defer func() {
		if status != nil {
			os.Remove(fp.Name())
		}
		fp.Close()
	}()

	bw := bufio.NewWriter(fp)

	save := func(ctx context.Context, r kv.Reader) error {
		if err := Export(ctx, r, bw); err != nil {
			return fmt.Errorf("could not export db content: %w", err)
		}
		return nil
	}
	if err := kv.WithReader(ctx, db, save); err != nil {
		return err
	}

	if err := bw.Flush(); err != nil {
		return fmt.Errorf("could not flush the bufio writer: %w", err)
	}
	if err := fp.Sync(); err != nil {
		return fmt.Errorf("could not sync the output file: %w", err)
	}
	if err := os.Rename(fp.Name(), abspath); err != nil {
		return fmt.Errorf("could not rename temp file to %q: %w", fp.Name(), err)
	}
	return nil
}
