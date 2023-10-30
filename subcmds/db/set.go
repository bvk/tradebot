// Copyright (c) 2023 BVK Chaitanya

package db

import (
	"context"
	"flag"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/bvkgo/kv/kvhttp"
	"github.com/bvkgo/tradebot/cli"
)

type Set struct {
	Flags

	fset *flag.FlagSet
}

func (c *Set) Run(ctx context.Context, args []string) error {
	if len(args) != 2 {
		return fmt.Errorf("needs two (key, value) arguments")
	}

	baseURL := &url.URL{
		Scheme: "http",
		Host:   net.JoinHostPort(c.ip, fmt.Sprintf("%d", c.port)),
		Path:   c.basePath,
	}
	client := &http.Client{
		Timeout: c.httpTimeout,
	}

	db := kvhttp.New(baseURL, client)
	tx, err := db.NewTransaction(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if err := tx.Set(ctx, args[0], strings.NewReader(args[1])); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	return nil
}

func (c *Set) Command() (*flag.FlagSet, cli.CmdFunc) {
	if c.fset == nil {
		c.fset = flag.NewFlagSet("set", flag.ContinueOnError)
		c.Flags.setFlags(c.fset)
	}
	return c.fset, cli.CmdFunc(c.Run)
}

func (c *Set) Synopsis() string {
	return "Updates the value for a key in the database"
}
