// Copyright (c) 2023 BVK Chaitanya

package db

import (
	"bytes"
	"context"
	"encoding/gob"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"

	"github.com/bvk/tradebot/cli"
	"github.com/bvk/tradebot/subcmds/cmdutil"
)

type Edit struct {
	cmdutil.DBFlags

	valueType string

	create bool

	editor string
}

func (c *Edit) Run(ctx context.Context, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("needs one (key) argument")
	}
	key := args[0]

	if len(c.valueType) == 0 {
		return fmt.Errorf("valueType flag is required")
	}
	value, err := TypeNameValue(c.valueType)
	if err != nil {
		return fmt.Errorf("invalid type name %q: %w", c.valueType, err)
	}

	db, closer, err := c.DBFlags.GetDatabase(ctx)
	if err != nil {
		return err
	}
	defer closer()

	tx, err := db.NewTransaction(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	v, err := tx.Get(ctx, key)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if !c.create {
			return os.ErrNotExist
		}
	}

	if v != nil {
		if err := gob.NewDecoder(v).Decode(value); err != nil {
			return fmt.Errorf("could not gob-decode value at key %q: %w", key, err)
		}
	}

	orig, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("could not json-marshal value: %w", err)
	}

	filep, err := os.CreateTemp(os.TempDir(), "edit")
	if err != nil {
		return fmt.Errorf("could not create temp file: %w", err)
	}
	defer os.Remove(filep.Name())

	if err := os.WriteFile(filep.Name(), orig, 0); err != nil {
		filep.Close()
		return fmt.Errorf("could not write to temp file: %w", err)
	}
	if err := filep.Close(); err != nil {
		return fmt.Errorf("could not close temp file: %w", err)
	}

	cmd := exec.CommandContext(ctx, c.editor, filep.Name())
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("could not open editor: %w", err)
	}

	modified, err := os.ReadFile(filep.Name())
	if err != nil {
		return fmt.Errorf("could not read temp file: %w", err)
	}
	if bytes.Compare(orig, modified) == 0 {
		return fmt.Errorf("content is not modified; key is not updated")
	}

	mv, _ := TypeNameValue(c.valueType)
	if err := json.Unmarshal(modified, mv); err != nil {
		return fmt.Errorf("could not json-unmarshal modified content to object: %w", err)
	}
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(mv); err != nil {
		return fmt.Errorf("could not gob-encode modified object: %w", err)
	}
	if err := tx.Set(ctx, key, &buf); err != nil {
		return fmt.Errorf("could not update the value at key %q: %w", key, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("could not commit changes to the db: %w", err)
	}
	return nil
}

func (c *Edit) Command() (*flag.FlagSet, cli.CmdFunc) {
	fset := flag.NewFlagSet("edit", flag.ContinueOnError)
	c.DBFlags.SetFlags(fset)
	fset.StringVar(&c.valueType, "value-type", "", "when non-empty unmarshals to the selected type")
	fset.StringVar(&c.editor, "editor", "vi", "default editor")
	fset.BoolVar(&c.create, "create", false, "when true, key will be created if it doesn't exist")
	return fset, cli.CmdFunc(c.Run)
}

func (c *Edit) Synopsis() string {
	return "Create or Edit a key-value pair in the database"
}
