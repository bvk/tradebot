// Copyright (c) 2023 BVK Chaitanya

package db

import (
	"flag"
	"time"
)

type Flags struct {
	port        int
	ip          string
	basePath    string
	httpTimeout time.Duration
}

func (f *Flags) setFlags(fset *flag.FlagSet) {
	fset.IntVar(&f.port, "port", 10000, "TCP port number for the db endpoint")
	fset.StringVar(&f.ip, "ip", "127.0.0.1", "TCP ip address for the db endpoint")
	fset.StringVar(&f.basePath, "base-path", "/db", "path to db api handler")
	fset.DurationVar(&f.httpTimeout, "http-timeout", 1*time.Second, "http client timeout")
}
