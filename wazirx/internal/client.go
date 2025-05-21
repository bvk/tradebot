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
	"net/http/cookiejar"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bvk/tradebot/exchange"
)

type Client struct {
	lifeCtx    context.Context
	lifeCancel context.CancelCauseFunc

	wg sync.WaitGroup

	opts Options

	key, secret string

	client *http.Client

	// timeAdjustment is positive when local time is found to be ahead of the
	// server time, in which case, this value must be subtracted from the local
	// time before the local time can be used as a timestamp in the signature
	// calculations.
	timeAdjustment atomic.Int64
}

// New creates a client object that can perform operations on WazirX exchange.
func New(ctx context.Context, key, secret string, opts *Options) (*Client, error) {
	if opts == nil {
		opts = new(Options)
	}
	if err := opts.Check(); err != nil {
		return nil, err
	}
	stime, err := getServerTime(ctx, opts)
	if err != nil {
		return nil, err
	}
	adjustment := time.Now().Sub(stime.Time)
	if adjustment >= opts.MaxTimeAdjustment {
		return nil, fmt.Errorf("local time is out-of-sync by %v with the server", adjustment)
	}

	jar, err := cookiejar.New(nil /* options */)
	if err != nil {
		slog.Error("could not create cookiejar", "err", err)
		return nil, fmt.Errorf("could not create cookiejar: %w", err)
	}

	lifeCtx, lifeCancel := context.WithCancelCause(context.Background())
	c := &Client{
		lifeCtx:    lifeCtx,
		lifeCancel: lifeCancel,

		opts:   *opts,
		key:    key,
		secret: secret,
		client: &http.Client{
			Jar:     jar,
			Timeout: opts.HttpClientTimeout,
		},
	}

	c.timeAdjustment.Store(int64(adjustment))
	c.wg.Add(1)
	go func() { c.goFindTimeAdjustment(c.lifeCtx); c.wg.Done() }()
	return c, nil
}

func (c *Client) Close() error {
	return nil
}

func (c *Client) goFindTimeAdjustment(ctx context.Context) {
	for {
		sctx, scancel := context.WithTimeout(ctx, time.Second)
		<-sctx.Done()
		scancel()

		if ctx.Err() != nil {
			return
		}

		stime, err := getServerTime(ctx, &c.opts)
		if err != nil {
			slog.Error("could not get server time (will retry)", "err", err)
			continue
		}

		diff := time.Now().Sub(stime.Time)
		c.timeAdjustment.Store(int64(diff))
		slog.Debug("updated local time adjustment", "adjustment", diff)
	}
}

// getServerTime returns exchange server's current time.
func getServerTime(ctx context.Context, opts *Options) (exchange.RemoteTime, error) {
	var zero exchange.RemoteTime

	addrURL := fmt.Sprintf("https://%s/sapi/v1/time", opts.RestHostname)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, addrURL, nil)
	if err != nil {
		return zero, err
	}
	client := http.Client{Timeout: opts.HttpClientTimeout}

	start := time.Now()
	resp, err := client.Do(req)
	stop := time.Now()
	if err != nil {
		return zero, err
	} else if resp.StatusCode != http.StatusOK {
		return zero, fmt.Errorf("get time failed with status code %d", resp.StatusCode)
	}
	defer resp.Body.Close()

	latency := stop.Sub(start)
	if latency > opts.MaxFetchTimeLatency {
		slog.Warn("get server time took too long", "latency", latency, "max-allowed", opts.MaxFetchTimeLatency)
		return zero, fmt.Errorf("get server time took too long (%v > %v)", latency, opts.MaxFetchTimeLatency)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		slog.Error("could not read server time response body", "err", err)
		return zero, err
	}

	type ServerTime struct {
		UnixMilli int64 `json:"serverTime"`
	}
	var st ServerTime
	if err := json.Unmarshal(body, &st); err != nil {
		return zero, err
	}
	stime := time.UnixMilli(st.UnixMilli).UTC()
	stime.Add(latency / 2)

	return exchange.RemoteTime{Time: stime}, nil
}

// Now returns current server-time.
func (c *Client) Now() exchange.RemoteTime {
	return exchange.RemoteTime{Time: time.Now().Add(time.Duration(-c.timeAdjustment.Load()))}
}

// GetExchangeInfo returns basic exchange information.
func (c *Client) GetExchangeInfo(ctx context.Context) (*GetExchangeInfoResponse, error) {
	url := &url.URL{
		Scheme: "https",
		Host:   c.opts.RestHostname,
		Path:   "/sapi/v1/exchangeInfo",
	}
	resp := new(GetExchangeInfoResponse)
	if err := getJSON(ctx, c, url, resp); err != nil {
		if !errors.Is(err, context.Canceled) {
			slog.Error("could not get exchange info details", "err", err)
		}
		return nil, err
	}
	return resp, nil
}

// GetFunds returns user account balances for all asset types.
func (c *Client) GetFunds(ctx context.Context) (*GetFundsResponse, error) {
	url := &url.URL{
		Scheme: "https",
		Host:   c.opts.RestHostname,
		Path:   "/sapi/v1/funds",
	}
	resp := new(GetFundsResponse)
	if err := sgetJSON(ctx, c, url, nil, resp); err != nil {
		if !errors.Is(err, context.Canceled) {
			slog.Error("could not get funds", "url", url, "err", err)
		}
		return nil, err
	}
	return resp, nil
}

// NOTE: This API is defined, but is not supported.
// func (c *Client) GetFundDetails(ctx context.Context) (*GetFundDetailsResponse, error) {
// 	url := &url.URL{
// 		Scheme: "https",
// 		Host:   c.opts.RestHostname,
// 		Path:   "/sapi/v1/sub_account/accounts",
// 	}
// 	resp := new(GetFundDetailsResponse)
// 	if err := sgetJSON(ctx, c, url, nil, resp); err != nil {
// 		if !errors.Is(err, context.Canceled) {
// 			slog.Error("could not get fund details", "url", url, "err", err)
// 		}
// 		return nil, err
// 	}
// 	return resp, nil
// }

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

	// data, err := io.ReadAll(resp.Body)
	// if err != nil {
	// 	return err
	// }
	// log.Printf("response body: %s", data)
	// body = bytes.NewReader(data)

	if err := json.NewDecoder(body).Decode(result); err != nil {
		slog.Error("could not decode response to json", "err", err)
		return err
	}
	return nil
}

func sgetJSON[PT *T, T any](ctx context.Context, c *Client, addrURL *url.URL, values url.Values, resultPtr PT) error {
	// Add timestamp item with up-to-date time.
	if values == nil {
		values = make(url.Values)
	}
	if values.Has("timestamp") {
		return fmt.Errorf("timestamp key should not be set: %w", os.ErrInvalid)
	}
	now := c.Now()
	values.Add("timestamp", fmt.Sprintf("%d", now.Time.UnixMilli()))
	input := values.Encode()

	// Compute the signature and attach it to the values.
	if values.Has("signature") {
		return fmt.Errorf("signature key should not be set: %w", os.ErrInvalid)
	}
	hash := hmac.New(sha256.New, []byte(c.secret))
	hash.Write([]byte(input))
	signature := hash.Sum(nil)
	input += fmt.Sprintf("&signature=%x", signature)

	log.Printf("key=%q", c.key)
	log.Printf("secret=%q", c.secret)
	log.Printf("input=%q", input)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, addrURL.String(), strings.NewReader(input))
	if err != nil {
		slog.Error("could not create http post request with context", "url", addrURL, "err", err)
		return err
	}

	req.Header.Add("X-API-KEY", c.key)
	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")

	s := time.Now()
	resp, err := c.client.Do(req)
	if d := time.Now().Sub(s); d > c.opts.HttpClientTimeout {
		slog.Warn(fmt.Sprintf("post request took %s which is more than the http client timeout %s", d, c.opts.HttpClientTimeout))
	}
	if err != nil {
		if !errors.Is(err, context.Canceled) {
			slog.Error("could not perform http post request", "url", addrURL, "err", err)
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
		slog.Warn("http post returned unsuccessful status code", "status-code", resp.StatusCode)
		if body, err := io.ReadAll(resp.Body); err == nil {
			log.Printf("server response was %s", body)
		}

		if resp.StatusCode == http.StatusBadGateway {
			if err := sleep(time.Second); err != nil {
				return err
			}
			return sgetJSON(ctx, c, addrURL, values, resultPtr)
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
			return sgetJSON(ctx, c, addrURL, values, resultPtr)
		}

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

func postJSON[PT *T, T any](ctx context.Context, c *Client, addrURL *url.URL, values url.Values, resultPtr PT) error {
	// Add timestamp item with up-to-date time.
	if values == nil {
		values = make(url.Values)
	}
	if values.Has("timestamp") {
		return fmt.Errorf("timestamp key should not be set: %w", os.ErrInvalid)
	}
	now := c.Now()
	values.Add("timestamp", fmt.Sprintf("%d", now.Time.UnixMilli()))
	input := values.Encode()

	// Compute the signature and attach it to the values.
	if values.Has("signature") {
		return fmt.Errorf("signature key should not be set: %w", os.ErrInvalid)
	}
	hash := hmac.New(sha256.New, []byte(c.secret))
	hash.Write([]byte(input))
	signature := hash.Sum(nil)
	input += fmt.Sprintf("&signature=%x", signature)

	log.Printf("key=%q", c.key)
	log.Printf("secret=%q", c.secret)
	log.Printf("input=%q", input)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, addrURL.String(), strings.NewReader(input))
	if err != nil {
		slog.Error("could not create http post request with context", "url", addrURL, "err", err)
		return err
	}

	req.Header.Add("X-API-KEY", c.key)
	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")

	s := time.Now()
	resp, err := c.client.Do(req)
	if d := time.Now().Sub(s); d > c.opts.HttpClientTimeout {
		slog.Warn(fmt.Sprintf("post request took %s which is more than the http client timeout %s", d, c.opts.HttpClientTimeout))
	}
	if err != nil {
		if !errors.Is(err, context.Canceled) {
			slog.Error("could not perform http post request", "url", addrURL, "err", err)
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
		slog.Warn("http post returned unsuccessful status code", "status-code", resp.StatusCode)
		if body, err := io.ReadAll(resp.Body); err == nil {
			log.Printf("server response was %s", body)
		}

		if resp.StatusCode == http.StatusBadGateway {
			if err := sleep(time.Second); err != nil {
				return err
			}
			return postJSON(ctx, c, addrURL, values, resultPtr)
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
			return postJSON(ctx, c, addrURL, values, resultPtr)
		}

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
