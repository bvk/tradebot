// Copyright (c) 2023 BVK Chaitanya

package looper

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"log"
	"regexp"
	"slices"
	"strings"

	"github.com/bvk/tradebot/cli"
	"github.com/bvk/tradebot/gobs"
	"github.com/bvk/tradebot/kvutil"
	"github.com/bvk/tradebot/looper"
	"github.com/bvk/tradebot/subcmds/cmdutil"
	"github.com/bvkgo/kv"
)

type List struct {
	cmdutil.DBFlags

	keyRe string

	dataType string

	printTemplate string
}

func (c *List) Run(ctx context.Context, args []string) error {
	if len(args) != 0 {
		return fmt.Errorf("this command takes no arguments")
	}

	var keyRe *regexp.Regexp
	if len(c.keyRe) != 0 {
		re, err := regexp.Compile(c.keyRe)
		if err != nil {
			return fmt.Errorf("could not compile key-regexp value: %w", err)
		}
		keyRe = re
	}

	dataTypes := []string{"state", "status"}
	if !slices.Contains(dataTypes, c.dataType) {
		return fmt.Errorf("invalid data-type %q", c.dataType)
	}

	var tmpl *template.Template
	if len(c.printTemplate) > 0 {
		t, err := template.New("print").Parse(c.printTemplate)
		if err != nil {
			return fmt.Errorf("could not parse print-template: %w", err)
		}
		tmpl = t
	}

	db, closer, err := c.DBFlags.GetDatabase(ctx)
	if err != nil {
		return err
	}
	defer closer()

	lister := func(ctx context.Context, r kv.Reader, k string, v *gobs.LooperState) error {
		if keyRe != nil && !keyRe.MatchString(k) {
			return nil
		}

		var value any
		switch c.dataType {
		case "state":
			value = v
		case "status":
			uid := strings.TrimPrefix(k, looper.DefaultKeyspace)
			t, err := looper.Load(ctx, uid, r)
			if err != nil {
				return fmt.Errorf("could not load looper instance at key %q: %w", k, err)
			}
			status := t.Status()
			if status == nil {
				log.Printf("looper at %q has nil status", k)
				return nil
			}
			value = status
		}

		if tmpl == nil {
			data, _ := json.Marshal(value)
			fmt.Printf("%s\n", data)
			return nil
		}

		var sb strings.Builder
		if err := tmpl.Execute(&sb, value); err != nil {
			return fmt.Errorf("could not execute print template against value at key %q: %w", k, err)
		}
		fmt.Printf("%s %s\n", k, sb.String())
		return nil
	}
	beg, end := kvutil.PathRange(looper.DefaultKeyspace)
	if err := kvutil.AscendDB(ctx, db, beg, end, lister); err != nil {
		return err
	}
	return nil
}

func (c *List) Command() (*flag.FlagSet, cli.CmdFunc) {
	fset := flag.NewFlagSet("list", flag.ContinueOnError)
	c.DBFlags.SetFlags(fset)
	fset.StringVar(&c.keyRe, "key-regexp", "", "regular expression to pick keys")
	fset.StringVar(&c.dataType, "data-type", "state", "one of state|status")
	fset.StringVar(&c.printTemplate, "print-template", "", "text/template to print the value")
	return fset, cli.CmdFunc(c.Run)
}

func (c *List) Synopsis() string {
	return "Lists buy-sell loop jobs under a keyspace"
}
