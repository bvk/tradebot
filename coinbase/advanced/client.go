// Copyright (c) 2023 BVK Chaitanya

package advanced

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"math/big"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path"
	"time"

	"github.com/bvk/tradebot/ctxutil"
	"github.com/bvk/tradebot/exchange"
	"github.com/visvasity/ntpsync"
	"golang.org/x/time/rate"

	jose "gopkg.in/square/go-jose.v2"
	"gopkg.in/square/go-jose.v2/jwt"
)

type Client struct {
	cg ctxutil.CloseGroup

	opts Options

	kid     string
	pemText string

	priKey *ecdsa.PrivateKey
	signer jose.Signer

	key    string
	secret []byte

	client *http.Client

	limiter *rate.Limiter
}

type nonceSource struct{}

func (n nonceSource) Nonce() (string, error) {
	r, err := rand.Int(rand.Reader, big.NewInt(math.MaxInt64))
	if err != nil {
		return "", err
	}
	return r.String(), nil
}

// New creates a client for coinbase exchange.
func New(ctx context.Context, kid, pemtext string, opts *Options) (*Client, error) {
	if opts == nil {
		opts = new(Options)
	}
	opts.setDefaults()

	block, _ := pem.Decode([]byte(pemtext))
	if block == nil {
		slog.Error("could not parse the PEM private key")
		return nil, os.ErrInvalid
	}
	priKey, err := x509.ParseECPrivateKey(block.Bytes)
	if err != nil {
		slog.Error("could not parse the EC private key", "err", err)
		return nil, err
	}
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.ES256, Key: priKey},
		(&jose.SignerOptions{NonceSource: nonceSource{}}).WithType("JWT").WithHeader("kid", kid),
	)
	if err != nil {
		slog.Error("could not create go-jose.v2 pkg signer", "err", err)
		return nil, err
	}

	jar, err := cookiejar.New(nil /* options */)
	if err != nil {
		slog.Error("could not create cookiejar", "err", err)
		return nil, fmt.Errorf("could not create cookiejar: %w", err)
	}

	c := &Client{
		opts:    *opts,
		kid:     kid,
		pemText: pemtext,
		priKey:  priKey,
		signer:  signer,
		client: &http.Client{
			Jar:     jar,
			Timeout: opts.HttpClientTimeout,
		},
		limiter: rate.NewLimiter(25, 1),
	}
	return c, nil
}

// Close shuts down the coinbase client.
func (c *Client) Close() error {
	c.cg.Close()
	return nil
}

func (c *Client) Now() exchange.RemoteTime {
	return exchange.RemoteTime{Time: ntpsync.Now()}
}

type APIKeyClaims struct {
	*jwt.Claims
	URI string `json:"uri"`
}

func (c *Client) signJWT(uri string) (string, error) {
	cl := &APIKeyClaims{
		Claims: &jwt.Claims{
			Subject:   c.kid,
			Issuer:    "cdp",
			NotBefore: jwt.NewNumericDate(ntpsync.Now()),
			Expiry:    jwt.NewNumericDate(ntpsync.Now().Add(2 * time.Minute)),
		},
		URI: uri,
	}
	return jwt.Signed(c.signer).Claims(cl).CompactSerialize()
}

// func (c *Client) sign(message string) string {
// 	signature := hmac.New(sha256.New, c.secret)
// 	_, err := signature.Write([]byte(message))
// 	if err != nil {
// 		slog.Error("could not write to hmac stream (ignored)", "err", err)
// 		return ""
// 	}
// 	sig := hex.EncodeToString(signature.Sum(nil))
// 	return sig
// }

func (c *Client) getJSON(ctx context.Context, url *url.URL, result interface{}) error {
	urlStr := url.String()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, urlStr, nil)
	if err != nil {
		slog.Error("could not create http get request with context", "url", urlStr, "err", err)
		return err
	}
	token, err := c.signJWT(fmt.Sprintf("%s %s%s", req.Method, req.URL.Host, req.URL.Path))
	if err != nil {
		slog.Error("could not create signed jwt token for GET", "url", urlStr, "err", err)
		return err
	}
	req.Header.Add("Authorization", "Bearer "+token)
	if err := c.limiter.Wait(ctx); err != nil {
		return err
	}
	at := time.Now()
	resp, err := c.client.Do(req)
	latency := time.Since(at)
	if latency > c.opts.HttpClientTimeout {
		slog.Warn("GET request took longer than the desired timeout", "desired", c.opts.HttpClientTimeout, "taken", latency)
	}
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
			return c.getJSON(ctx, url, result)
		}
		if resp.StatusCode == http.StatusTooManyRequests {
			slog.Warn(fmt.Sprintf("get request returned with status code 429 - too many requests (retrying)"))
			return c.getJSON(ctx, url, result)
		}
		slog.Error("http GET is unsuccessful", "status", resp.StatusCode, "url", url.String())
		return fmt.Errorf("http GET returned %d", resp.StatusCode)
	}
	var body io.Reader = resp.Body
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	body = bytes.NewReader(data)
	slog.Debug("Coinbase GET", "url", urlStr, "latency", latency, "response", string(data))

	if err := json.NewDecoder(body).Decode(result); err != nil {
		slog.Error("could not decode response to json", "err", err)
		return err
	}
	return nil
}

func (c *Client) postJSON(ctx context.Context, url *url.URL, request, resultPtr interface{}) error {
	urlStr := url.String()
	payload, err := json.Marshal(request)
	if err != nil {
		slog.Error("could not marshal post request body to json", "err", err)
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, urlStr, bytes.NewReader(payload))
	if err != nil {
		slog.Error("could not create http post request with context", "url", urlStr, "err", err)
		return err
	}
	token, err := c.signJWT(fmt.Sprintf("%s %s%s", req.Method, req.URL.Host, req.URL.Path))
	if err != nil {
		slog.Error("could not create signed jwt token for POST", "url", urlStr, "err", err)
		return err
	}
	req.Header.Add("Authorization", "Bearer "+token)
	if err := c.limiter.Wait(ctx); err != nil {
		return err
	}
	at := time.Now()
	resp, err := c.client.Do(req)
	latency := time.Since(at)
	if latency > c.opts.HttpClientTimeout {
		slog.Warn("POST request took longer than the desired timeout", "desired", c.opts.HttpClientTimeout, "taken", latency)
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
			return c.postJSON(ctx, url, request, resultPtr)
		}
		if resp.StatusCode == http.StatusTooManyRequests {
			slog.Warn(fmt.Sprintf("post request returned with status code 429 - too many requests (retrying)"))
			return c.postJSON(ctx, url, request, resultPtr)
		}
		slog.Error("http POST is unsuccessful", "status", resp.StatusCode)
		return fmt.Errorf("http POST returned %d", resp.StatusCode)
	}
	var body io.Reader = resp.Body
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	body = bytes.NewReader(data)
	slog.Debug("Coinbase POST", "url", urlStr, "latency", latency, "request", string(payload), "response", string(data))

	if err := json.NewDecoder(body).Decode(resultPtr); err != nil {
		slog.Error("could not decode response to json", "err", err)
		return err
	}
	return nil
}

func (c *Client) Do(ctx context.Context, method string, url *url.URL, payload interface{}) (*http.Response, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		slog.Error("could not marshal user payload to json", "url", url, "err", err)
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, method, url.String(), bytes.NewReader(data))
	if err != nil {
		slog.Error("could not create http request object with context", "method", method, "url", url, "err", err)
		return nil, err
	}
	token, err := c.signJWT(fmt.Sprintf("%s %s%s", method, url.Host, url.Path))
	if err != nil {
		slog.Error("could not create signed jwt token", "method", method, "err", err)
		return nil, err
	}
	req.Header.Add("Authorization", "Bearer "+token)

	if err := c.limiter.Wait(ctx); err != nil {
		return nil, err
	}
	return c.client.Do(req)
}

func (c *Client) Go(f func(context.Context)) {
	c.cg.Go(f)
}

func (c *Client) AfterDurationFunc(d time.Duration, f func(context.Context)) {
	c.cg.AfterDurationFunc(d, f)
}

func (c *Client) GetOrder(ctx context.Context, orderID string) (*GetOrderResponse, error) {
	url := &url.URL{
		Scheme: "https",
		Host:   c.opts.RestHostname,
		Path:   "/api/v3/brokerage/orders/historical/" + orderID,
	}
	resp := new(GetOrderResponse)
	if err := c.getJSON(ctx, url, resp); err != nil {
		if !errors.Is(err, context.Canceled) {
			slog.Error("could not http get order details", "order", orderID, "err", err)
		}
		return nil, err
	}
	return resp, nil
}

func (c *Client) GetAccount(ctx context.Context, uuid string) (*GetAccountResponse, error) {
	url := &url.URL{
		Scheme: "https",
		Host:   c.opts.RestHostname,
		Path:   "/api/v3/brokerage/accounts/" + uuid,
	}
	resp := new(GetAccountResponse)
	if err := c.getJSON(ctx, url, resp); err != nil {
		if !errors.Is(err, context.Canceled) {
			slog.Error("could not http get account details", "account", uuid, "err", err)
		}
		return nil, err
	}
	return resp, nil
}

func (c *Client) ListAccounts(ctx context.Context, values url.Values) (_ *ListAccountsResponse, cont url.Values, _ error) {
	url := &url.URL{
		Scheme:   "https",
		Host:     c.opts.RestHostname,
		Path:     "/api/v3/brokerage/accounts",
		RawQuery: values.Encode(),
	}
	resp := new(ListAccountsResponse)
	if err := c.getJSON(ctx, url, resp); err != nil {
		if !errors.Is(err, context.Canceled) {
			slog.Error("could not http list accounts", "url", url, "err", err)
		}
		return nil, nil, err
	}
	if len(resp.Cursor) > 0 {
		values.Set("cursor", resp.Cursor)
		return resp, values, nil
	}
	return resp, nil, nil
}

func (c *Client) ListFills(ctx context.Context, values url.Values) (_ *ListFillsResponse, cont url.Values, _ error) {
	url := &url.URL{
		Scheme:   "https",
		Host:     c.opts.RestHostname,
		Path:     "/api/v3/brokerage/orders/historical/fills",
		RawQuery: values.Encode(),
	}
	resp := new(ListFillsResponse)
	if err := c.getJSON(ctx, url, resp); err != nil {
		if !errors.Is(err, context.Canceled) {
			slog.Error("could not http list fills", "url", url, "err", err)
		}
		return nil, nil, err
	}
	if len(resp.Cursor) > 0 {
		values.Set("cursor", resp.Cursor)
		return resp, values, nil
	}
	return resp, nil, nil
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
		if !errors.Is(err, context.Canceled) {
			slog.Error("could not list orders", "url", url, "err", err)
		}
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
		if !errors.Is(err, context.Canceled) {
			slog.Error("could not get product details", "url", url, "product", productID, "err", err)
		}
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
		if !errors.Is(err, context.Canceled) {
			slog.Error("could not list products", "url", url, "err", err)
		}
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
		if !errors.Is(err, context.Canceled) {
			slog.Error("could not create order", "url", url, "err", err)
		}
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
		slog.Error("could not cancel order", "url", url, "err", err)
		return nil, err
	}
	return resp, nil
}

func (c *Client) EditOrder(ctx context.Context, request *EditOrderRequest) (*EditOrderResponse, error) {
	url := &url.URL{
		Scheme: "https",
		Host:   c.opts.RestHostname,
		Path:   "/api/v3/brokerage/orders/edit",
	}
	resp := new(EditOrderResponse)
	if err := c.postJSON(ctx, url, request, resp); err != nil {
		if !errors.Is(err, context.Canceled) {
			slog.Error("could not edit order", "url", url, "err", err)
		}
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
		if !errors.Is(err, context.Canceled) {
			slog.Error("could not get product candles", "url", url, "err", err)
		}
		return nil, fmt.Errorf("could not http-get product candles %q: %w", productID, err)
	}
	return resp, nil
}
