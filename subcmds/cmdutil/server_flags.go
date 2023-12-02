// Copyright (c) 2023 BVK Chaitanya

package cmdutil

import (
	"flag"
)

type ServerFlags struct {
	Port int
	IP   string
}

func (sf *ServerFlags) SetFlags(fset *flag.FlagSet) {
	fset.IntVar(&sf.Port, "listen-port", 10000, "TCP port number for the api endpoint")
	fset.StringVar(&sf.IP, "listen-ip", "127.0.0.1", "TCP ip address for the api endpoint")
}
