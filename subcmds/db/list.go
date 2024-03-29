// Copyright (c) 2023 BVK Chaitanya

package db

import (
	"context"
	"encoding/gob"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"regexp"
	"strings"
	"text/template"

	"github.com/bvk/tradebot/cli"
	"github.com/bvk/tradebot/gobs"
	"github.com/bvk/tradebot/subcmds/cmdutil"
	"github.com/bvkgo/kv"
)

type List struct {
	cmdutil.DBFlags

	keyRe string

	valueType string

	printTemplate string

	inOrder, descend bool
}

func (c *List) Command() (*flag.FlagSet, cli.CmdFunc) {
	fset := flag.NewFlagSet("list", flag.ContinueOnError)
	c.DBFlags.SetFlags(fset)
	fset.StringVar(&c.keyRe, "key-regexp", "", "regular expression to pick keys")
	fset.StringVar(&c.valueType, "value-type", "", "gob type name for the values")
	fset.StringVar(&c.printTemplate, "print-template", "", "text/template to print the value")
	fset.BoolVar(&c.inOrder, "in-order", false, "when true, prints in ascending order")
	fset.BoolVar(&c.descend, "descend", false, "when true, prints in descending order")
	return fset, cli.CmdFunc(c.Run)
}

func (c *List) Synopsis() string {
	return "Prints keys and values in the database"
}

func (c *List) Run(ctx context.Context, args []string) error {
	if len(args) != 0 {
		return fmt.Errorf("command takes no arguments")
	}

	var keyRe *regexp.Regexp
	if len(c.keyRe) != 0 {
		re, err := regexp.Compile(c.keyRe)
		if err != nil {
			return fmt.Errorf("could not compile key-regexp value: %w", err)
		}
		keyRe = re
	}

	if len(c.valueType) != 0 {
		if _, err := gobs.NewByTypename(c.valueType); err != nil {
			return fmt.Errorf("invalid value-type %q: %w", c.valueType, err)
		}
	}

	var tmpl *template.Template
	if len(c.printTemplate) > 0 {
		t, err := template.New("print").Parse(c.printTemplate)
		if err != nil {
			return fmt.Errorf("could not parse print-template: %w", err)
		}
		tmpl = t
	}

	list := func(ctx context.Context, r kv.Reader) error {
		var it kv.Iterator
		var err error
		if !c.inOrder && !c.descend {
			it, err = r.Scan(ctx)
		} else if c.descend {
			it, err = r.Descend(ctx, "", "")
		} else {
			it, err = r.Ascend(ctx, "", "")
		}
		if err != nil {
			return err
		}
		defer kv.Close(it)

		for k, v, err := it.Fetch(ctx, false); err == nil; k, v, err = it.Fetch(ctx, true) {
			if keyRe == nil {
				fmt.Println(k)
				continue
			}

			if !keyRe.MatchString(k) {
				continue
			}

			if c.valueType == "" {
				fmt.Println(k)
				continue
			}

			value, _ := gobs.NewByTypename(c.valueType)
			if err := gob.NewDecoder(v).Decode(value); err != nil {
				return fmt.Errorf("could not gob-decode value for key %q: %w", k, err)
			}

			if tmpl == nil {
				data, _ := json.Marshal(value)
				fmt.Printf("%s\n", data)
				continue
			}

			var sb strings.Builder
			if err := tmpl.Execute(&sb, value); err != nil {
				return fmt.Errorf("could not execute print template against value at key %q: %w", k, err)
			}
			fmt.Printf("%s %s\n", k, sb.String())
		}

		if _, _, err := it.Fetch(ctx, false); err != nil && !errors.Is(err, io.EOF) {
			return err
		}
		return nil
	}

	db, closer, err := c.DBFlags.GetDatabase(ctx)
	if err != nil {
		return err
	}
	defer closer()

	if err := kv.WithReader(ctx, db, list); err != nil {
		return err
	}
	return nil
}
