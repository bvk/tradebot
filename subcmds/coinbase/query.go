// Copyright (c) 2026 BVK Chaitanya

package coinbase

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"text/template"

	"github.com/bvk/tradebot/coinbase/advanced"
	"github.com/visvasity/cli"
)

type Query struct {
	inputFile string

	format string
}

func (c *Query) Command() (string, *flag.FlagSet, cli.CmdFunc) {
	fset := new(flag.FlagSet)
	fset.StringVar(&c.inputFile, "input-file", "", "path to coinbase download output file")
	fset.StringVar(&c.format, "format", "", "Golang text/template form to print data from each order")
	return "query", fset, cli.CmdFunc(c.run)
}

func (c *Query) run(ctx context.Context, args []string) error {
	if len(c.inputFile) == 0 {
		return fmt.Errorf("input file is required")
	}
	if len(c.format) == 0 {
		return fmt.Errorf("print format template is required")
	}

	tmpl, err := template.New("print").Parse(c.format)
	if err != nil {
		return fmt.Errorf("could not parse print-template: %w", err)
	}

	inp, err := os.Open(c.inputFile)
	if err != nil {
		return err
	}
	defer inp.Close()

	r := bufio.NewReader(inp)
	line, err := r.ReadBytes('\n')
	for ; err == nil; line, err = r.ReadBytes('\n') {
		order := new(advanced.Order)
		if err := json.Unmarshal(line, order); err != nil {
			return err
		}
		var sb strings.Builder
		if err := tmpl.Execute(&sb, order); err != nil {
			return fmt.Errorf("could not execute text/template: %w", err)
		}
		fmt.Println(sb.String())
	}

	return nil
}
