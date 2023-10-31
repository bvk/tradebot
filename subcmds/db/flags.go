// Copyright (c) 2023 BVK Chaitanya

package db

import (
	"flag"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/bvkgo/kv"
	"github.com/bvkgo/kv/kvhttp"
)

type Flags struct {
	port        int
	ip          string
	basePath    string
	httpTimeout time.Duration
}

func (f *Flags) check() error {
	// TODO: Add checks.
	return nil
}

func (f *Flags) SetFlags(fset *flag.FlagSet) {
	fset.IntVar(&f.port, "port", 10000, "TCP port number for the db endpoint")
	fset.StringVar(&f.ip, "ip", "127.0.0.1", "TCP ip address for the db endpoint")
	fset.StringVar(&f.basePath, "base-path", "/db", "path to db api handler")
	fset.DurationVar(&f.httpTimeout, "http-timeout", 1*time.Second, "http client timeout")
}

func (f *Flags) Client() kv.Database {
	baseURL := &url.URL{
		Scheme: "http",
		Host:   net.JoinHostPort(f.ip, fmt.Sprintf("%d", f.port)),
		Path:   f.basePath,
	}
	client := &http.Client{
		Timeout: f.httpTimeout,
	}

	return kvhttp.New(baseURL, client)
}
