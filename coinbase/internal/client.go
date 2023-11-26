// Copyright (c) 2023 BVK Chaitanya

package internal

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"path"
	"time"

	"github.com/bvk/tradebot/ctxutil"
	"github.com/bvk/tradebot/exchange"
	"golang.org/x/time/rate"
)

type Client struct {
	cg ctxutil.CloseGroup

	opts Options

	key    string
	secret []byte

	client *http.Client

	limiter *rate.Limiter

	// timeAdjustment is positive when local time is found to be ahead of the
	// server time, in which case, this value must be subtracted from the local
	// time before the local time can be used as a timestamp in the signature
	// calculations.
	timeAdjustment time.Duration
}

// New creates a client for coinbase exchange.
func New(key, secret string, opts *Options) (*Client, error) {
	if opts == nil {
		opts = new(Options)
	}
	opts.setDefaults()

	adjustment, err := findTimeAdjustment(context.Background())
	if err != nil {
		return nil, err
	}
	log.Printf("local time needs to be adjusted by -%s to match the coinbase server time", adjustment)
	if adjustment > opts.MaxTimeAdjustment {
		return nil, fmt.Errorf("local time is out-of-sync by large amount with the server time")
	}

	jar, err := cookiejar.New(nil /* options */)
	if err != nil {
		return nil, fmt.Errorf("could not create cookiejar: %w", err)
	}

	c := &Client{
		opts:   *opts,
		key:    key,
		secret: []byte(secret),
		client: &http.Client{
			Jar:     jar,
			Timeout: opts.HttpClientTimeout,
		},
		limiter:        rate.NewLimiter(25, 1),
		timeAdjustment: adjustment,
	}

	return c, nil
}

// Close shuts down the coinbase client.
func (c *Client) Close() error {
	c.cg.Close()
	return nil
}

func findTimeAdjustment(ctx context.Context) (time.Duration, error) {
	type ServerTime struct {
		ISO string `json:"iso"`
	}

	for ; ctx.Err() == nil; time.Sleep(time.Second) {
		start := time.Now()
		resp, err := http.Get("https://api.exchange.coinbase.com/time")
		stop := time.Now()

		latency := stop.Sub(start)
		if latency > 100*time.Millisecond {
			continue // retry
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return 0, fmt.Errorf("could not ready server time response: %w", err)
		}

		var st ServerTime
		if err := json.Unmarshal(body, &st); err != nil {
			return 0, fmt.Errorf("could not unmarshal server time response: %w", err)
		}

		stime, err := time.Parse("2006-01-02T15:04:05.999Z", st.ISO)
		if err != nil {
			return 0, fmt.Errorf("could not parse server timestamp: %w", err)
		}

		ltime := start.Add(latency / 2)
		adjust := ltime.Sub(stime)
		if adjust < 0 {
			return 0, nil
		}
		return adjust, nil
	}

	return 0, context.Cause(ctx)
}

func (c *Client) Now() exchange.RemoteTime {
	return exchange.RemoteTime{Time: time.Now().Add(-c.timeAdjustment)}
}

func (c *Client) sign(message string) string {
	signature := hmac.New(sha256.New, c.secret)
	_, err := signature.Write([]byte(message))
	if err != nil {
		slog.Error("could not write to hmac stream (ignored)", "error", err)
		return ""
	}
	sig := hex.EncodeToString(signature.Sum(nil))
	return sig
}

func (c *Client) getJSON(ctx context.Context, url *url.URL, result interface{}) error {
	at := fmt.Sprintf("%d", c.Now().Unix())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url.String(), nil)
	if err != nil {
		return err
	}
	sdata := fmt.Sprintf("%s%s%s%s", at, req.Method, url.Path, "")
	signature := c.sign(sdata)
	req.Header.Add("Content-Type", "application/json")
	req.Header.Add("Cache-Control", "no-store")
	req.Header.Add("CB-ACCESS-KEY", c.key)
	req.Header.Add("CB-ACCESS-SIGN", signature)
	req.Header.Add("CB-ACCESS-TIMESTAMP", at)
	if err := c.limiter.Wait(ctx); err != nil {
		return err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
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
		slog.Error("could not decode response to json", "error", err)
		return err
	}
	return nil
}

func (c *Client) postJSON(ctx context.Context, url *url.URL, request, resultPtr interface{}) error {
	payload, err := json.Marshal(request)
	if err != nil {
		return err
	}
	at := fmt.Sprintf("%d", c.Now().Unix())
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url.String(), bytes.NewReader(payload))
	if err != nil {
		return err
	}
	sdata := fmt.Sprintf("%s%s%s%s", at, req.Method, url.Path, payload)
	signature := c.sign(sdata)
	req.Header.Add("Content-Type", "application/json")
	req.Header.Add("Cache-Control", "no-store")
	req.Header.Add("CB-ACCESS-KEY", c.key)
	req.Header.Add("CB-ACCESS-SIGN", signature)
	req.Header.Add("CB-ACCESS-TIMESTAMP", at)
	if err := c.limiter.Wait(ctx); err != nil {
		return err
	}
	s := time.Now()
	resp, err := c.client.Do(req)
	if d := time.Now().Sub(s); d > c.opts.HttpClientTimeout {
		log.Printf("warning: post request took %s which is more than the http client timeout %s", d, c.opts.HttpClientTimeout)
	}
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
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
		slog.Error("could not decode response to json", "error", err)
		return err
	}
	return nil
}

func (c *Client) Do(ctx context.Context, method string, url *url.URL, payload interface{}) (*http.Response, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	at := fmt.Sprintf("%d", c.Now().Unix())
	req, err := http.NewRequestWithContext(ctx, method, url.String(), bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	sdata := fmt.Sprintf("%s%s%s%s", at, req.Method, url.Path, data)
	signature := c.sign(sdata)
	req.Header.Add("Content-Type", "application/json")
	req.Header.Add("Cache-Control", "no-store")
	req.Header.Add("CB-ACCESS-KEY", c.key)
	req.Header.Add("CB-ACCESS-SIGN", signature)
	req.Header.Add("CB-ACCESS-TIMESTAMP", at)

	if err := c.limiter.Wait(ctx); err != nil {
		return nil, err
	}
	return c.client.Do(req)
}

func (c *Client) Go(f func(context.Context)) {
	c.cg.Go(f)
}

func (c *Client) GetOrder(ctx context.Context, orderID string) (*GetOrderResponse, error) {
	url := &url.URL{
		Scheme: "https",
		Host:   c.opts.RestHostname,
		Path:   "/api/v3/brokerage/orders/historical/" + orderID,
	}
	resp := new(GetOrderResponse)
	if err := c.getJSON(ctx, url, resp); err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *Client) ListOrders(ctx context.Context, values url.Values) (_ *ListOrdersResponse, cont url.Values, _ error) {
	url := &url.URL{
		Scheme:   "https",
		Host:     c.opts.RestHostname,
		Path:     "/api/v3/brokerage/orders/historical/batch",
		RawQuery: values.Encode(),
	}
	resp := new(ListOrdersResponse)
	if err := c.getJSON(ctx, url, resp); err != nil {
		return nil, nil, err
	}
	if len(resp.Cursor) > 0 {
		values.Set("cursor", resp.Cursor)
		return resp, values, nil
	}
	return resp, nil, nil
}

func (c *Client) GetProduct(ctx context.Context, productID string) (*GetProductResponse, error) {
	url := &url.URL{
		Scheme: "https",
		Host:   c.opts.RestHostname,
		Path:   path.Join("/api/v3/brokerage/products/", productID),
	}
	resp := new(GetProductResponse)
	if err := c.getJSON(ctx, url, resp); err != nil {
		return nil, fmt.Errorf("could not http-get product %q: %w", productID, err)
	}
	return resp, nil
}

func (c *Client) ListProducts(ctx context.Context, productType string) (*ListProductsResponse, error) {
	values := make(url.Values)
	values.Set("product_type", productType)

	url := &url.URL{
		Scheme:   "https",
		Host:     c.opts.RestHostname,
		Path:     "/api/v3/brokerage/products",
		RawQuery: values.Encode(),
	}
	resp := new(ListProductsResponse)
	if err := c.getJSON(ctx, url, resp); err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *Client) CreateOrder(ctx context.Context, request *CreateOrderRequest) (*CreateOrderResponse, error) {
	url := &url.URL{
		Scheme: "https",
		Host:   c.opts.RestHostname,
		Path:   "/api/v3/brokerage/orders",
	}
	resp := new(CreateOrderResponse)
	if err := c.postJSON(ctx, url, request, resp); err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *Client) CancelOrder(ctx context.Context, request *CancelOrderRequest) (*CancelOrderResponse, error) {
	url := &url.URL{
		Scheme: "https",
		Host:   c.opts.RestHostname,
		Path:   "/api/v3/brokerage/orders/batch_cancel",
	}
	resp := new(CancelOrderResponse)
	if err := c.postJSON(ctx, url, request, resp); err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *Client) GetProductCandles(ctx context.Context, productID string, values url.Values) (*GetProductCandlesResponse, error) {
	url := &url.URL{
		Scheme:   "https",
		Host:     c.opts.RestHostname,
		Path:     path.Join("/api/v3/brokerage/products/", productID, "/candles"),
		RawQuery: values.Encode(),
	}
	resp := new(GetProductCandlesResponse)
	if err := c.getJSON(ctx, url, resp); err != nil {
		return nil, fmt.Errorf("could not http-get product candles %q: %w", productID, err)
	}
	return resp, nil
}
