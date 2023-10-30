// Copyright (c) 2023 BVK Chaitanya

package db

import (
	"context"
	"flag"
	"fmt"
	"net"
	"net/http"
	"net/url"

	"github.com/bvkgo/kv/kvhttp"
	"github.com/bvkgo/tradebot/cli"
)

type Delete struct {
	Flags
}

func (c *Delete) Run(ctx context.Context, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("needs one (key) argument")
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

	if err := tx.Delete(ctx, args[0]); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	return nil
}

func (c *Delete) Command() (*flag.FlagSet, cli.CmdFunc) {
	fset := flag.NewFlagSet("delete", flag.ContinueOnError)
	c.Flags.SetFlags(fset)
	return fset, cli.CmdFunc(c.Run)
}

func (c *Delete) Synopsis() string {
	return "Deletes a key in the database"
}
