// Copyright (c) BVK Chaitanya

package internal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"time"
)

type Client struct {
	opts Options

	key, secret string

	client *http.Client
}

func New(ctx context.Context, key, secret string, opts *Options) (*Client, error) {
	if opts == nil {
		opts = new(Options)
	}
	if err := opts.Check(); err != nil {
		return nil, err
	}
	c := &Client{
		opts:   *opts,
		key:    key,
		secret: secret,
		client: &http.Client{
			Timeout: opts.HttpClientTimeout,
		},
	}
	return c, nil
}

func (c *Client) Close() error {
	return nil
}

func (c *Client) ListMarkets(ctx context.Context) (*ListMarketsResponse, error) {
	values := make(url.Values)
	url := &url.URL{
		Scheme:   "https",
		Host:     c.opts.RestHostname,
		Path:     "/exchange/v1/markets",
		RawQuery: values.Encode(),
	}
	resp := new(ListMarketsResponse)
	if err := getJSON(ctx, c, url, resp); err != nil {
		if !errors.Is(err, context.Canceled) {
			slog.Error("could not list products", "url", url, "err", err)
		}
		return nil, err
	}
	return resp, nil
}

func getJSON[PT *T, T any](ctx context.Context, c *Client, url *url.URL, result PT) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url.String(), nil)
	if err != nil {
		slog.Error("could not create http get request with context", "url", url, "err", err)
		return err
	}

	resp, err := c.client.Do(req)
	if err != nil {
		if !errors.Is(err, context.Canceled) {
			slog.Error("could not do http client request", "err", err)
		}
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusBadGateway {
			slog.Warn(fmt.Sprintf("get request returned with status code 429 - too many requests (retrying after timeout)"))
			time.Sleep(time.Second)
			return getJSON(ctx, c, url, result)
		}
		if resp.StatusCode == http.StatusTooManyRequests {
			slog.Warn(fmt.Sprintf("get request returned with status code 429 - too many requests (retrying)"))
			return getJSON(ctx, c, url, result)
		}
		slog.Error("http GET is unsuccessful", "status", resp.StatusCode, "url", url.String())
		return fmt.Errorf("http GET returned %d", resp.StatusCode)
	}
	var body io.Reader = resp.Body

	if err := json.NewDecoder(body).Decode(result); err != nil {
		slog.Error("could not decode response to json", "err", err)
		return err
	}
	return nil
}
