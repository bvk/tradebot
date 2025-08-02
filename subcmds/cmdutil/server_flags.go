// Copyright (c) 2023 BVK Chaitanya

package cmdutil

import (
	"flag"

	"github.com/bvk/tradebot/subcmds/defaults"
)

type ServerFlags struct {
	port int
	IP   string
}

func (sf *ServerFlags) SetFlags(fset *flag.FlagSet) {
	fset.IntVar(&sf.port, "listen-port", defaults.ServerPort(), "TCP port number for the api endpoint")
	fset.StringVar(&sf.IP, "listen-ip", "127.0.0.1", "TCP ip address for the api endpoint")
}

func (sf *ServerFlags) Port() int {
	return sf.port
}
