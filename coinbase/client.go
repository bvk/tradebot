// Copyright (c) 2023 BVK Chaitanya

package coinbase

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"sync"
	"time"
)

type Client struct {
	ctx    context.Context
	cancel context.CancelCauseFunc
	wg     sync.WaitGroup

	opts Options

	key    string
	secret []byte

	client     *http.Client
	websockets []*Websocket

	// TODO: Save full *ProductType objects.
	spotProducts []string

	// orderIDMap keeps a mapping between client order ids to server order ids.
	orderIDMap map[string]string

	pollCh chan struct{}
}

// New creates a client for coinbase exchange.
func New(key, secret string, opts *Options) (*Client, error) {
	if opts == nil {
		opts = new(Options)
	}
	opts.setDefaults()

	ctx, cancel := context.WithCancelCause(context.Background())
	c := &Client{
		ctx:        ctx,
		cancel:     cancel,
		opts:       *opts,
		key:        key,
		secret:     []byte(secret),
		orderIDMap: make(map[string]string),
		client: &http.Client{
			Timeout: opts.HttpClientTimeout,
		},
	}

	// fetch product list.
	products, err := c.listProducts(c.ctx)
	if err != nil {
		cancel(os.ErrClosed)
		return nil, err
	}
	c.spotProducts = products

	c.wg.Add(1)
	go c.goWatchOrders()
	return c, nil
}

// Close shuts down the coinbase client.
func (c *Client) Close() error {
	c.cancel(os.ErrClosed)
	c.wg.Wait()

	for len(c.websockets) > 0 {
		c.CloseWebsocket(c.websockets[0])
	}
	return nil
}

func (c *Client) sleep(d time.Duration) error {
	select {
	case <-time.After(d):
		return nil
	case <-c.ctx.Done():
		return c.ctx.Err()
	}
}

func (c *Client) listProducts(ctx context.Context) ([]string, error) {
	values := make(url.Values)
	values.Set("product_type", "SPOT")

	url := &url.URL{
		Scheme:   "https",
		Host:     c.opts.RestHostname,
		Path:     "/api/v3/brokerage/products",
		RawQuery: values.Encode(),
	}
	resp := new(ListProductsResponse)
	if err := c.httpGetJSON(ctx, url, resp); err != nil {
		return nil, err
	}
	var ids []string
	for _, p := range resp.Products {
		ids = append(ids, p.ProductID)
	}
	return ids, nil
}

func (c *Client) httpGetJSON(ctx context.Context, url *url.URL, result interface{}) error {
	at := time.Now()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url.String(), nil)
	if err != nil {
		return err
	}
	sdata := fmt.Sprintf("%s%s%s%s", strconv.FormatInt(at.Unix(), 10), req.Method, url.Path, "")
	signature := c.sign(sdata)
	req.Header.Add("Content-Type", "application/json")
	req.Header.Add("CB-ACCESS-KEY", c.key)
	req.Header.Add("CB-ACCESS-SIGN", signature)
	req.Header.Add("CB-ACCESS-TIMESTAMP", strconv.FormatInt(at.Unix(), 10))
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
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
	if err := json.NewDecoder(body).Decode(result); err != nil {
		slog.Error("could not decode response to json", "error", err)
		return err
	}
	return nil
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

func (c *Client) goWatchOrders() {
	defer c.wg.Done()

	for c.ctx.Err() == nil {
		if err := c.watchOrders(c.ctx); err != nil {
			if c.ctx.Err() == nil {
				slog.WarnContext(c.ctx, "websocket session closed (can retry)", "error", err)
				c.sleep(c.opts.WebsocketRetryInterval)
			}
		}
	}
}

func (c *Client) watchOrders(ctx context.Context) error {
	ws, err := c.NewWebsocket(ctx, c.spotProducts)
	if err != nil {
		return err
	}
	defer c.CloseWebsocket(ws)

	if err := ws.Subscribe(ctx, "user"); err != nil {
		return err
	}
	defer ws.Unsubscribe(ctx, "user")

	for ctx.Err() == nil {
		msg, err := ws.NextMessage(ctx)
		if err != nil {
			return err
		}
		slog.Info("user channel", "msg", msg)
	}
	return nil
}

func (c *Client) goPollOrders() {
	defer c.wg.Done()

	for c.ctx.Err() == nil {
		select {
		case <-c.ctx.Done():
			return
		case <-c.pollCh:
			if err := c.pollOrders(c.ctx); err != nil {
				if c.ctx.Err() == nil {
					slog.WarnContext(c.ctx, "could not poll for orders", "error", err)
					c.sleep(c.opts.PollOrdersRetryInterval)
				}
			}
		}
	}
}

func (c *Client) pollOrders(ctx context.Context) error {
	return nil
}

func (c *Client) listOrders(ctx context.Context, values url.Values) (_ *ListOrdersResponse, cont url.Values, _ error) {
	url := &url.URL{
		Scheme:   "https",
		Host:     c.opts.RestHostname,
		Path:     "/api/v3/brokerage/orders/historical/batch",
		RawQuery: values.Encode(),
	}
	resp := new(ListOrdersResponse)
	if err := c.httpGetJSON(ctx, url, resp); err != nil {
		return nil, nil, err
	}
	if len(resp.Cursor) > 0 {
		values.Set("cursor", resp.Cursor)
		return resp, values, nil
	}
	return resp, nil, nil
}

func (c *Client) getOrder(ctx context.Context, orderID string) (*GetOrderResponse, error) {
	url := &url.URL{
		Scheme: "https",
		Host:   c.opts.RestHostname,
		Path:   "/api/v3/brokerage/orders/historical/" + orderID,
	}
	resp := new(GetOrderResponse)
	if err := c.httpGetJSON(ctx, url, resp); err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *Client) createOrder(ctx context.Context, request *CreateOrderRequest) (*CreateOrderResponse, error) {
	// Make sure that client-order-id is unique.
	if len(request.ClientOrderID) > 0 {
		if _, ok := c.orderIDMap[request.ClientOrderID]; ok {
			return nil, fmt.Errorf("client order id is already in use")
		}
	}

	url := &url.URL{
		Scheme: "https",
		Host:   c.opts.RestHostname,
		Path:   "/api/v3/brokerage/orders",
	}
	resp := new(CreateOrderResponse)
	if err := c.httpPostJSON(ctx, url, request, resp); err != nil {
		return nil, err
	}

	if resp.Success && len(request.ClientOrderID) > 0 {
		c.orderIDMap[request.ClientOrderID] = resp.OrderID
	}
	return resp, nil
}

func (c *Client) cancelOrder(ctx context.Context, request *CancelOrderRequest) (*CancelOrderResponse, error) {
	url := &url.URL{
		Scheme: "https",
		Host:   c.opts.RestHostname,
		Path:   "/api/v3/brokerage/orders/batch_cancel",
	}
	resp := new(CancelOrderResponse)
	if err := c.httpPostJSON(ctx, url, request, resp); err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *Client) httpPostJSON(ctx context.Context, url *url.URL, request, resultPtr interface{}) error {
	payload, err := json.Marshal(request)
	if err != nil {
		return err
	}

	at := time.Now()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url.String(), bytes.NewReader(payload))
	if err != nil {
		return err
	}
	sdata := fmt.Sprintf("%s%s%s%s", strconv.FormatInt(at.Unix(), 10), req.Method, url.Path, payload)
	signature := c.sign(sdata)
	req.Header.Add("Content-Type", "application/json")
	req.Header.Add("CB-ACCESS-KEY", c.key)
	req.Header.Add("CB-ACCESS-SIGN", signature)
	req.Header.Add("CB-ACCESS-TIMESTAMP", strconv.FormatInt(at.Unix(), 10))
	resp, err := c.client.Do(req)
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

	at := time.Now()
	req, err := http.NewRequestWithContext(ctx, method, url.String(), bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	sdata := fmt.Sprintf("%s%s%s%s", strconv.FormatInt(at.Unix(), 10), req.Method, url.Path, data)
	signature := c.sign(sdata)
	req.Header.Add("Content-Type", "application/json")
	req.Header.Add("CB-ACCESS-KEY", c.key)
	req.Header.Add("CB-ACCESS-SIGN", signature)
	req.Header.Add("CB-ACCESS-TIMESTAMP", strconv.FormatInt(at.Unix(), 10))
	return c.client.Do(req)
}
