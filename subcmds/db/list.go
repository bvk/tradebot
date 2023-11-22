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
	"github.com/bvkgo/kv"
)

type List struct {
	Flags

	keyRe string

	valueType string

	printTemplate string
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
		if _, err := TypeNameValue(c.valueType); err != nil {
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
		it, err := r.Scan(ctx)
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

			value, _ := TypeNameValue(c.valueType)
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

	db, err := c.Flags.GetDatabase(ctx)
	if err != nil {
		return err
	}
	if err := kv.WithReader(ctx, db, list); err != nil {
		return err
	}
	return nil
}

func (c *List) Command() (*flag.FlagSet, cli.CmdFunc) {
	fset := flag.NewFlagSet("list", flag.ContinueOnError)
	c.Flags.SetFlags(fset)
	fset.StringVar(&c.keyRe, "key-regexp", "", "regular expression to pick keys")
	fset.StringVar(&c.valueType, "value-type", "", "gob type name for the values")
	fset.StringVar(&c.printTemplate, "print-template", "", "text/template to print the value")
	return fset, cli.CmdFunc(c.Run)
}

func (c *List) Synopsis() string {
	return "Prints keys and values in the database"
}
