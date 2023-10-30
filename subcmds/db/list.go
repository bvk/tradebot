// Copyright (c) 2023 BVK Chaitanya

package db

import (
	"context"
	"flag"
	"fmt"
	"net"
	"net/http"
	"net/url"

	"github.com/bvkgo/kv"
	"github.com/bvkgo/kv/kvhttp"
	"github.com/bvkgo/tradebot/cli"
)

type List struct {
	Flags

	printValues bool
}

func (c *List) Run(ctx context.Context, args []string) error {
	baseURL := &url.URL{
		Scheme: "http",
		Host:   net.JoinHostPort(c.ip, fmt.Sprintf("%d", c.port)),
		Path:   c.basePath,
	}
	client := &http.Client{
		Timeout: c.httpTimeout,
	}

	// TODO: handle printValues flag

	list := func(ctx context.Context, r kv.Reader) error {
		it, err := r.Scan(ctx)
		if err != nil {
			return err
		}
		defer kv.Close(it)

		for k, _, ok := it.Current(ctx); ok; k, _, ok = it.Next(ctx) {
			fmt.Println(k)
		}

		if err := it.Err(); err != nil {
			return err
		}
		return nil
	}

	db := kvhttp.New(baseURL, client)
	if err := kv.WithReader(ctx, db, list); err != nil {
		return err
	}
	return nil
}

func (c *List) Command() (*flag.FlagSet, cli.CmdFunc) {
	fset := flag.NewFlagSet("list", flag.ContinueOnError)
	c.Flags.setFlags(fset)
	fset.BoolVar(&c.printValues, "print-values", false, "values are printed when true")
	return fset, cli.CmdFunc(c.Run)
}

func (c *List) Synopsis() string {
	return "Prints keys and values in the database"
}
