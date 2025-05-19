// Copyright (c) BVK Chaitanya

package internal

import (
	"bytes"
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
			slog.Error("could not list markets", "url", url, "err", err)
		}
		return nil, err
	}
	return resp, nil
}

func (c *Client) ListMarketDetails(ctx context.Context) (*ListMarketDetailsResponse, error) {
	values := make(url.Values)
	url := &url.URL{
		Scheme:   "https",
		Host:     c.opts.RestHostname,
		Path:     "/exchange/v1/markets_details",
		RawQuery: values.Encode(),
	}
	resp := new(ListMarketDetailsResponse)
	if err := getJSON(ctx, c, url, resp); err != nil {
		if !errors.Is(err, context.Canceled) {
			slog.Error("could not list market details", "url", url, "err", err)
		}
		return nil, err
	}
	return resp, nil
}

func (c *Client) GetBalances(ctx context.Context) (*GetBalancesResponse, error) {
	url := &url.URL{
		Scheme: "https",
		Host:   c.opts.RestHostname,
		Path:   "/exchange/v1/users/balances",
	}
	resp := new(GetBalancesResponse)
	if err := postJSON(ctx, c, url, nil, resp); err != nil {
		if !errors.Is(err, context.Canceled) {
			slog.Error("could not retrieve user balances", "url", url, "err", err)
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

func postJSON[PT *T, T any](ctx context.Context, c *Client, url *url.URL, request any, resultPtr PT) error {
	var requestPtr any
	now := time.Now()
	requestPtr = &AnySignedRequest{
		UnixMilli: now.UnixMilli(),
	}
	if request != nil {
		requestPtr = request
	}

	payload, err := json.Marshal(requestPtr)
	if err != nil {
		slog.Error("could not marshal post request body to json", "err", err)
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url.String(), bytes.NewReader(payload))
	if err != nil {
		slog.Error("could not create http post request with context", "url", url, "err", err)
		return err
	}

	hash := hmac.New(sha256.New, []byte(c.secret))
	hash.Write(payload)
	signature := hash.Sum(nil)

	req.Header.Add("Content-Type", "application/json")
	req.Header.Add("X-AUTH-APIKEY", c.key)
	req.Header.Add("X-AUTH-SIGNATURE", fmt.Sprintf("%x", signature))

	s := time.Now()
	resp, err := c.client.Do(req)
	if d := time.Now().Sub(s); d > c.opts.HttpClientTimeout {
		slog.Warn(fmt.Sprintf("post request took %s which is more than the http client timeout %s", d, c.opts.HttpClientTimeout))
	}
	if err != nil {
		if !errors.Is(err, context.Canceled) {
			slog.Error("could not perform http post request", "url", url, "err", err)
		}
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusBadGateway {
			slog.Error(fmt.Sprintf("get request returned with status code 429 - too many requests (retrying after timeout)"))
			time.Sleep(time.Second)
			return postJSON(ctx, c, url, request, resultPtr)
		}
		if resp.StatusCode == http.StatusTooManyRequests {
			slog.Warn(fmt.Sprintf("post request returned with status code 429 - too many requests (retrying)"))
			return postJSON(ctx, c, url, request, resultPtr)
		}
		data, _ := io.ReadAll(resp.Body)
		log.Printf("failure response=%s", data)
		slog.Error("http POST is unsuccessful", "status", resp.StatusCode)
		return fmt.Errorf("http POST returned %d", resp.StatusCode)
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
	if err := json.NewDecoder(body).Decode(resultPtr); err != nil {
		slog.Error("could not decode response to json", "err", err)
		return err
	}
	return nil
}
