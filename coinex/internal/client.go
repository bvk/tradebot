// Copyright (c) 2025 BVK Chaitanya

package internal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path"
	"strconv"
	"time"
)

type Client struct {
	lifeCtx    context.Context
	lifeCancel context.CancelCauseFunc

	opts Options

	client http.Client
}

// New returns a new client instance.
func New(opts *Options) (*Client, error) {
	if opts == nil {
		opts = new(Options)
	}
	opts.setDefaults()
	if err := opts.Check(); err != nil {
		return nil, err
	}

	lifeCtx, lifeCancel := context.WithCancelCause(context.Background())
	c := &Client{
		opts:       *opts,
		lifeCtx:    lifeCtx,
		lifeCancel: lifeCancel,
		client: http.Client{
			Timeout: opts.HttpClientTimeout,
		},
	}
	return c, nil
}

// Close releases resources and destroys the client instance.
func (c *Client) Close() error {
	c.lifeCancel(os.ErrClosed)
	return nil
}

func (c *Client) GetMarkets(ctx context.Context) (*GetMarketsResponse, error) {
	addrURL := &url.URL{
		Scheme: RestURL.Scheme,
		Host:   RestURL.Host,
		Path:   path.Join(RestURL.Path, "/spot/market"),
	}
	resp := new(GetMarketsResponse)
	if err := httpGetJSON(ctx, c, addrURL, resp); err != nil {
		if !errors.Is(err, context.Canceled) {
			slog.Error("could not get market status", "url", addrURL, "err", err)
		}
		return nil, err
	}
	return resp, nil
}

func (c *Client) GetMarketInfo(ctx context.Context, market string) (*GetMarketInfoResponse, error) {
	values := make(url.Values)
	values.Set("market", market)

	addrURL := &url.URL{
		Scheme:   RestURL.Scheme,
		Host:     RestURL.Host,
		Path:     path.Join(RestURL.Path, "/spot/ticker"),
		RawQuery: values.Encode(),
	}
	resp := new(GetMarketInfoResponse)
	if err := httpGetJSON(ctx, c, addrURL, resp); err != nil {
		if !errors.Is(err, context.Canceled) {
			slog.Error("could not get market ticker information", "url", addrURL, "err", err)
		}
		return nil, err
	}
	return resp, nil
}

func httpGetJSON[PT *T, T any](ctx context.Context, c *Client, addrURL *url.URL, responsePtr PT) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, addrURL.String(), nil)
	if err != nil {
		slog.Error("could not create http get request with context", "url", addrURL, "err", err)
		return err
	}

	s := time.Now()
	resp, err := c.client.Do(req)
	if d := time.Now().Sub(s); d > c.opts.HttpClientTimeout {
		slog.Warn(fmt.Sprintf("get request took %s which is more than the http client timeout %s", d, c.opts.HttpClientTimeout))
	}
	if err != nil {
		if !errors.Is(err, context.Canceled) {
			slog.Error("could not perform http get request", "url", addrURL, "err", err)
		}
		return err
	}
	defer resp.Body.Close()

	sleep := func(d time.Duration) error {
		sctx, scancel := context.WithTimeout(ctx, d)
		<-sctx.Done()
		scancel()
		if ctx.Err() != nil {
			return context.Cause(ctx)
		}
		return nil
	}

	if resp.StatusCode != http.StatusOK {
		slog.Warn("http get returned unsuccessful status code", "status-code", resp.StatusCode)
		if body, err := io.ReadAll(resp.Body); err == nil {
			log.Printf("server response was %s", body)
		}

		if resp.StatusCode == http.StatusBadGateway {
			if err := sleep(time.Second); err != nil {
				return err
			}
			return httpGetJSON(ctx, c, addrURL, responsePtr)
		}

		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusTeapot {
			timeout := time.Second
			if x := resp.Header.Get("Retry-After"); len(x) != 0 {
				if v, err := strconv.Atoi(x); err == nil {
					timeout = time.Duration(v) * time.Second
				}
			}

			if err := sleep(timeout); err != nil {
				return err
			}
			return httpGetJSON(ctx, c, addrURL, responsePtr)
		}

		slog.Error("http GET is unsuccessful", "status", resp.StatusCode)
		return fmt.Errorf("http GET returned %d", resp.StatusCode)
	}

	var body io.Reader = resp.Body
	/////
	// data, err := ioutil.ReadAll(resp.Body)
	// if err != nil {
	// 	return err
	// }
	// slog.Info("response body", "data", data)
	// body = bytes.NewReader(data)
	/////
	if err := json.NewDecoder(body).Decode(responsePtr); err != nil {
		slog.Error("could not decode response to json", "err", err)
		return err
	}
	return nil
}
