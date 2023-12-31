// Copyright (c) 2023 BVK Chaitanya

package cmdutil

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path"
	"time"
)

type ClientFlags struct {
	Port        int
	Host        string
	APIPath     string
	HTTPTimeout time.Duration
}

func (cf *ClientFlags) SetFlags(fset *flag.FlagSet) {
	fset.IntVar(&cf.Port, "connect-port", 10000, "TCP port number for the api endpoint")
	fset.StringVar(&cf.Host, "connect-host", "127.0.0.1", "Hostname or IP address for the api endpoint")
	fset.StringVar(&cf.APIPath, "api-path", "/", "base path to the api handler")
	fset.DurationVar(&cf.HTTPTimeout, "http-timeout", 30*time.Second, "http client timeout")
}

func (cf *ClientFlags) AddressURL() *url.URL {
	return &url.URL{
		Scheme: "http",
		Host:   net.JoinHostPort(cf.Host, fmt.Sprintf("%d", cf.Port)),
		Path:   cf.APIPath,
	}
}

func (cf *ClientFlags) HttpClient() *http.Client {
	return &http.Client{
		Timeout: cf.HTTPTimeout,
	}
}

func Post[RESP, REQ any](ctx context.Context, cf *ClientFlags, subpath string, req *REQ) (*RESP, error) {
	data, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	addrURL := cf.AddressURL()
	addrURL.Path = path.Join(addrURL.Path, subpath)
	r, err := http.NewRequestWithContext(ctx, http.MethodPost, addrURL.String(), bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	r.Header.Set("content-type", "application/json")

	client := cf.HttpClient()
	resp, err := client.Do(r)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("http status code %d: %s", resp.StatusCode, data)
	}
	response := new(RESP)
	if err := json.NewDecoder(resp.Body).Decode(response); err != nil {
		return nil, err
	}
	return response, nil
}
