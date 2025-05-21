// Copyright (c) 2025 BVK Chaitanya

package internal

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
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
	"strings"
	"time"
)

type Client struct {
	lifeCtx    context.Context
	lifeCancel context.CancelCauseFunc

	opts Options

	client http.Client

	key, secret string
}

// New returns a new client instance.
func New(key, secret string, opts *Options) (*Client, error) {
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
		key:        key,
		secret:     secret,
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

func (c *Client) GetBalances(ctx context.Context) (*GetBalancesResponse, error) {
	addrURL := &url.URL{
		Scheme: RestURL.Scheme,
		Host:   RestURL.Host,
		Path:   path.Join(RestURL.Path, "/assets/spot/balance"),
	}
	resp := new(GetBalancesResponse)
	if err := privateGetJSON(ctx, c, addrURL, nil /* request */, resp); err != nil {
		if !errors.Is(err, context.Canceled) {
			slog.Error("could not get asset balances", "url", addrURL, "err", err)
		}
		return nil, err
	}
	return resp, nil
}

func (c *Client) do(ctx context.Context, method string, addrURL *url.URL, body string) (*http.Response, error) {
	var sb strings.Builder
	sb.WriteString(method)
	sb.WriteString(addrURL.Path)
	if len(addrURL.RawQuery) != 0 {
		sb.WriteRune('?')
		sb.WriteString(addrURL.RawQuery)
	}
	if body != "" {
		sb.WriteString(body)
	}

	now := time.Now()
	timestamp := strconv.FormatInt(now.UnixMilli(), 10)
	sb.WriteString(timestamp)

	hash := hmac.New(sha256.New, []byte(c.secret))
	io.WriteString(hash, sb.String())
	signature := hash.Sum(nil)

	req, err := http.NewRequestWithContext(ctx, method, addrURL.String(), strings.NewReader(body))
	if err != nil {
		slog.Error("could not create http request object with context", "method", method, "url", addrURL, "err", err)
		return nil, err
	}

	req.Header.Add("X-COINEX-KEY", c.key)
	req.Header.Add("X-COINEX-SIGN", fmt.Sprintf("%x", signature))
	req.Header.Add("X-COINEX-TIMESTAMP", timestamp)
	return c.client.Do(req)
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

func sleep(ctx context.Context, d time.Duration) error {
	sctx, scancel := context.WithTimeout(ctx, d)
	<-sctx.Done()
	scancel()
	if ctx.Err() != nil {
		return context.Cause(ctx)
	}
	return nil
}

func privateGetJSON[PT *T, T any](ctx context.Context, c *Client, addrURL *url.URL, request any, response PT) error {
	var sb strings.Builder
	if request != nil {
		if err := json.NewEncoder(&sb).Encode(request); err != nil {
			return err
		}
	}

	resp, err := c.do(ctx, http.MethodGet, addrURL, sb.String())
	if err != nil {
		if !errors.Is(err, context.Canceled) {
			slog.Error("could not perform http get request", "url", addrURL, "err", err)
		}
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		slog.Warn("http get returned unsuccessful status code", "status-code", resp.StatusCode)
		if body, err := io.ReadAll(resp.Body); err == nil {
			log.Printf("server response was %s", body)
		}

		if resp.StatusCode == http.StatusBadGateway {
			if err := sleep(ctx, time.Second); err != nil {
				return err
			}
			return privateGetJSON(ctx, c, addrURL, request, response)
		}

		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusTeapot {
			timeout := time.Second
			if x := resp.Header.Get("Retry-After"); len(x) != 0 {
				if v, err := strconv.Atoi(x); err == nil {
					timeout = time.Duration(v) * time.Second
				}
			}

			if err := sleep(ctx, timeout); err != nil {
				return err
			}
			return privateGetJSON(ctx, c, addrURL, request, response)
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
	if err := json.NewDecoder(body).Decode(response); err != nil {
		slog.Error("could not decode response to json", "err", err)
		return err
	}
	return nil
}
