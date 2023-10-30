// Copyright (c) 2023 BVK Chaitanya

package subcmds

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"path"
	"time"
)

type ServerFlags struct {
	port int
	ip   string
}

func (sf *ServerFlags) SetFlags(fset *flag.FlagSet) {
	fset.IntVar(&sf.port, "server-port", 10000, "TCP port number for the api endpoint")
	fset.StringVar(&sf.ip, "server-ip", "127.0.0.1", "TCP ip address for the api endpoint")
}

type ClientFlags struct {
	ServerFlags
	basePath    string
	httpTimeout time.Duration
}

func (cf *ClientFlags) SetFlags(fset *flag.FlagSet) {
	cf.ServerFlags.SetFlags(fset)
	fset.StringVar(&cf.basePath, "server-path", "/", "base path to the api handler")
	fset.DurationVar(&cf.httpTimeout, "client-timeout", 1*time.Second, "http client timeout")
}

func Post[RESP, REQ any](ctx context.Context, cf *ClientFlags, subpath string, req *REQ) (*RESP, error) {
	addrURL := &url.URL{
		Scheme: "http",
		Host:   net.JoinHostPort(cf.ip, fmt.Sprintf("%d", cf.port)),
		Path:   path.Join(cf.basePath, subpath),
	}
	client := &http.Client{
		Timeout: cf.httpTimeout,
	}
	data, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	r, err := http.NewRequestWithContext(ctx, http.MethodPost, addrURL.String(), bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	r.Header.Set("content-type", "application/json")
	resp, err := client.Do(r)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("received non-ok http status %d", resp.StatusCode)
	}
	response := new(RESP)
	if err := json.NewDecoder(resp.Body).Decode(response); err != nil {
		return nil, err
	}
	return response, nil
}
