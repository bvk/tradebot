// Copyright (c) 2023 BVK Chaitanya

package db

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"

	"github.com/bvkgo/kv/kvhttp"
	"github.com/bvkgo/tradebot/cli"
)

type Get struct {
	Flags
}

func (c *Get) Run(ctx context.Context, args []string) error {
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
	snap, err := db.NewSnapshot(ctx)
	if err != nil {
		return err
	}
	defer snap.Discard(ctx)

	v, err := snap.Get(ctx, args[0])
	if err != nil {
		return err
	}
	data, err := io.ReadAll(v)
	if err != nil {
		return err
	}

	fmt.Printf("%x", data)
	return nil
}

func (c *Get) Command() (*flag.FlagSet, cli.CmdFunc) {
	fset := flag.NewFlagSet("get", flag.ContinueOnError)
	c.Flags.SetFlags(fset)
	return fset, cli.CmdFunc(c.Run)
}

func (c *Get) Synopsis() string {
	return "Prints the value of a key in the database"
}
