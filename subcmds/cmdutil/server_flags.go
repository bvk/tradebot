// Copyright (c) 2023 BVK Chaitanya

package cmdutil

import (
	"flag"
	"os"
	"strconv"
)

type ServerFlags struct {
	port int
	IP   string
}

func (sf *ServerFlags) SetFlags(fset *flag.FlagSet) {
	fset.IntVar(&sf.port, "listen-port", 0, "TCP port number for the api endpoint (TRADEBOT_SERVER_PORT or 10000")
	fset.StringVar(&sf.IP, "listen-ip", "127.0.0.1", "TCP ip address for the api endpoint")
}

func (sf *ServerFlags) Port() int {
	if sf.port != 0 {
		return sf.port
	}
	if v := os.Getenv("TRADEBOT_SERVER_PORT"); len(v) != 0 {
		if port, err := strconv.ParseInt(v, 10, 16); err == nil {
			return int(port)
		}
	}
	return 10000
}
