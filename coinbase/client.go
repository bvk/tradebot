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

	// concurrent map[string]*orderData
	orderDataMap sync.Map

	// concurrent map[string]*orderData
	oldOrderDataMap sync.Map
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

func (c *Client) orderStatus(id string) (string, time.Time, bool) {
	value, ok := c.orderDataMap.Load(id)
	if !ok {
		v, ok := c.oldOrderDataMap.Load(id)
		if !ok {
			return "", time.Time{}, false
		}
		value = v
	}
	data := value.(*orderData)
	status, timestamp := data.status()
	return status, timestamp, true
}

func (c *Client) waitForStatusChange(ctx context.Context, id string, last time.Time) error {
	value, ok := c.orderDataMap.Load(id)
	if !ok {
		return os.ErrNotExist
	}
	data := value.(*orderData)

	sub, subCh, err := data.statusTopic.Subscribe(1)
	if err != nil {
		return err
	}
	defer sub.Unsubscribe()

	_, timestamp := data.status()
	for !last.Before(timestamp) {
		select {
		case <-ctx.Done():
			return context.Cause(ctx)
		case v := <-subCh:
			timestamp = v.localTime
		}
	}
	return nil
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
			if ctx.Err() != nil {
				break
			}
			return err
		}

		// All `user` channel messages are important, so we don't enforce sequence
		// ordering.

		if msg.Channel == "user" {
			if err := c.handleUserChannelMsg(ctx, msg); err != nil {
				slog.WarnContext(ctx, "could not handle user channel message (ignored)", "error", err)
			}
		}
	}
	return nil
}

func (c *Client) handleUserChannelMsg(ctx context.Context, msg *MessageType) error {
	serverTime, err := time.Parse(time.RFC3339Nano, msg.Timestamp)
	if err != nil {
		return err
	}

	for _, event := range msg.Events {
		if event.Type == "snapshot" {
			for _, order := range event.Orders {
				data := newOrderData(order.Status, serverTime)
				if old, ok := c.orderDataMap.LoadOrStore(order.OrderID, data); ok {
					data.Close()
					old.(*orderData).setStatus(order.Status, serverTime)
					continue
				}
			}
		}

		if event.Type == "update" {
			for _, order := range event.Orders {
				data := newOrderData(order.Status, serverTime)
				if old, ok := c.orderDataMap.LoadOrStore(order.OrderID, data); ok {
					data.Close()
					old.(*orderData).setStatus(order.Status, serverTime)
					continue
				}
			}
		}
	}
	return nil
}

func (c *Client) retireOldOrders() {
}

func (c *Client) goPollOrders() {
	defer c.wg.Done()

	// Question: Are there cases when websocket is down for soo long?
	//
	// When a websocket is down, OPEN orders may get FILLED and the corresponding
	// event may never show up in the new websocket data. Any user waiting on
	// such order will get stuck without periodic polling logic.

	// When we create an order, coinbase api returns an order-id, but that
	// doesn't guarantee the order is active. It may take somemore time before
	// our order becomes active (i.e., status=OPEN).
	//
	// If we want to make sure that our order is active, we have two options:
	//
	// 1. Use GetOrder api to fetch order status in a "loop" till it is active
	//
	//   This requires (a) at least two or more http requests (2) a loop till
	//   order becomes active, which means, a time.Sleep with arbitrary
	//   time.Duration between the iterations
	//
	// 2. Listen to the "user" channel on the websocket for the status changes
	//
	//   This requires us to handle lost notifications when websocket is dropped.
	//   It may also happen that websocket connection is stuck (not receiving
	//   data) and is not immediately identified due to tcp timeouts, in which
	//   case we need to fallback to GetOrder api.
	//
	// Given the above mentioned issues, we use a mixed approach. Whenever we
	// create an order, we hope to receive a notification from the websocket in a
	// given timeout. If no notification is received from the websocket we poll
	// for the order status using the GetOrder api.
	//
	// POLLING
	//
	// Client object maintains status => list of OrderIDs mapping for each kind
	// of order status. Every order created is placed in a mapping representing
	// order ids with UNKOWN status. Also, when an order is completed, it will be
	// removed from all mappings, so that mappings are bounded by number of live
	// orders.
	//
	// An asynchronous goroutine is used to poll for the status of order
	// ids. This goroutine is awakened whenever we need to use GetOrder api to
	// fetch for one or more orders' statuses.
	//
	// In the healthy case, websocket receives order status notifications and
	// immediately *moves* order ids into their target status and wakes up any
	// waiters. If the asynchronous goroutine wakes up it typically finds that no
	// order ids are present in the UNKNOWN status mapping.

	// for c.ctx.Err() == nil {
	// 	select {
	// 	case <-c.ctx.Done():
	// 		return
	// 	case <-c.pollCh:
	// 		if err := c.pollOrders(c.ctx); err != nil {
	// 			if c.ctx.Err() == nil {
	// 				slog.WarnContext(c.ctx, "could not poll for orders", "error", err)
	// 				c.sleep(c.opts.PollOrdersRetryInterval)
	// 			}
	// 		}
	// 	}
	// }
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
		data := newOrderData("", time.Time{})
		if _, ok := c.orderDataMap.LoadOrStore(resp.OrderID, data); ok {
			data.Close()
		}
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
