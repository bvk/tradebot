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
	"log"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path"
	"sync"
	"time"

	"github.com/bvk/tradebot/exchange"
	"golang.org/x/time/rate"
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

	limiter *rate.Limiter

	// oldFilled and oldCancelled maps hold mapping from client-id to the
	// corresponding order.
	oldFilled    map[string]*OrderType
	oldCancelled map[string]*OrderType

	// TODO: Save full *ProductType objects.
	spotProducts []string

	// concurrent map[string]*orderData
	orderDataMap sync.Map

	// concurrent map[string]*orderData
	oldOrderDataMap sync.Map

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

	jar, err := cookiejar.New(nil /* options */)
	if err != nil {
		return nil, fmt.Errorf("could not create cookiejar: %w", err)
	}

	ctx, cancel := context.WithCancelCause(context.Background())
	c := &Client{
		ctx:    ctx,
		cancel: cancel,
		opts:   *opts,
		key:    key,
		secret: []byte(secret),
		client: &http.Client{
			Jar:     jar,
			Timeout: opts.HttpClientTimeout,
		},
		limiter:        rate.NewLimiter(25, 1),
		oldFilled:      make(map[string]*OrderType),
		oldCancelled:   make(map[string]*OrderType),
		timeAdjustment: adjustment,
	}

	// fetch product list.
	products, err := c.listProducts(c.ctx)
	if err != nil {
		cancel(os.ErrClosed)
		return nil, fmt.Errorf("could not list products: %w", err)
	}
	c.spotProducts = products

	// fetch old filled and canceled orders.
	from := time.Now().Add(-24 * time.Hour)
	filled, err := c.listOldOrders(ctx, from, "FILLED")
	if err != nil {
		return nil, fmt.Errorf("could not list old filled orders: %w", err)
	}
	for _, v := range filled {
		if len(v.ClientOrderID) > 0 {
			c.oldFilled[v.ClientOrderID] = v
		}
	}
	log.Printf("fetched %d filled orders from %s", len(filled), from)

	cancelled, err := c.listOldOrders(ctx, from, "CANCELLED")
	if err != nil {
		return nil, fmt.Errorf("could not list old canceled orders: %w", err)
	}
	for _, v := range cancelled {
		if len(v.ClientOrderID) > 0 {
			c.oldCancelled[v.ClientOrderID] = v
		}
	}
	log.Printf("fetched %d cancelled orders from %s", len(cancelled), from)

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

func (c *Client) getProduct(ctx context.Context, productID string) (*GetProductResponse, error) {
	url := &url.URL{
		Scheme: "https",
		Host:   c.opts.RestHostname,
		Path:   path.Join("/api/v3/brokerage/products/", productID),
	}
	resp := new(GetProductResponse)
	if err := c.httpGetJSON(ctx, url, resp); err != nil {
		return nil, fmt.Errorf("could not http-get product %q: %w", productID, err)
	}
	return resp, nil
}

type CandleGranularity time.Duration

const (
	OneMinuteCandle     = CandleGranularity(time.Minute)
	FiveMinuteCandle    = CandleGranularity(5 * time.Minute)
	FifteenMinuteCandle = CandleGranularity(15 * time.Minute)
	ThirtyMinuteCandle  = CandleGranularity(30 * time.Minute)
	OneHourCandle       = CandleGranularity(time.Hour)
	TwoHourCandle       = CandleGranularity(2 * time.Hour)
	SixHourCandle       = CandleGranularity(6 * time.Hour)
	OneDayCandle        = CandleGranularity(24 * time.Hour)
)

func (c *Client) getProductCandles(ctx context.Context, productID string, from time.Time, granularity CandleGranularity) (*GetProductCandlesResponse, error) {
	end := from.Add(300 * time.Duration(granularity))

	g := ""
	switch granularity {
	case OneMinuteCandle:
		g = "ONE_MINUTE"
	case FiveMinuteCandle:
		g = "FIVE_MINUTE"
	case FifteenMinuteCandle:
		g = "FIFTEEN_MINUTE"
	case ThirtyMinuteCandle:
		g = "THIRTY_MINUTE"
	case OneHourCandle:
		g = "ONE_HOUR"
	case TwoHourCandle:
		g = "TWO_HOUR"
	case SixHourCandle:
		g = "SIX_HOUR"
	case OneDayCandle:
		g = "ONE_DAY"
	}

	values := make(url.Values)
	values.Set("start", fmt.Sprintf("%d", from.Unix()))
	values.Set("end", fmt.Sprintf("%d", end.Unix()))
	values.Set("granularity", g)

	url := &url.URL{
		Scheme:   "https",
		Host:     c.opts.RestHostname,
		Path:     path.Join("/api/v3/brokerage/products/", productID, "/candles"),
		RawQuery: values.Encode(),
	}
	resp := new(GetProductCandlesResponse)
	if err := c.httpGetJSON(ctx, url, resp); err != nil {
		return nil, fmt.Errorf("could not http-get product candles %q: %w", productID, err)
	}
	return resp, nil
}

func (c *Client) listOldOrders(ctx context.Context, from time.Time, status string) ([]*OrderType, error) {
	var result []*OrderType

	values := make(url.Values)
	values.Add("limit", "100")
	values.Add("start_date", from.Format(time.RFC3339))
	values.Add("order_status", status)
	for i := 0; i == 0 || values != nil; i++ {
		resp, cont, err := c.listOrders(ctx, values)
		if err != nil {
			return nil, err
		}
		values = cont

		result = append(result, resp.Orders...)
	}
	return result, nil
}

func (c *Client) now() exchange.RemoteTime {
	return exchange.RemoteTime{Time: time.Now().Add(-c.timeAdjustment)}
}

func (c *Client) httpGetJSON(ctx context.Context, url *url.URL, result interface{}) error {
	at := fmt.Sprintf("%d", c.now().Unix())
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
				if old, ok := c.orderDataMap.Load(string(order.OrderID)); ok {
					old.(*orderData).websocketUpdate(serverTime, order)
					continue
				}
				data := c.newOrderData(order.OrderID)
				data.websocketUpdate(serverTime, order)
				if old, ok := c.orderDataMap.LoadOrStore(string(order.OrderID), data); ok {
					data.Close()
					old.(*orderData).websocketUpdate(serverTime, order)
				}
			}
		}

		if event.Type == "update" {
			for _, order := range event.Orders {
				if old, ok := c.orderDataMap.Load(string(order.OrderID)); ok {
					old.(*orderData).websocketUpdate(serverTime, order)
					continue
				}
				data := c.newOrderData(order.OrderID)
				data.websocketUpdate(serverTime, order)
				if old, ok := c.orderDataMap.LoadOrStore(string(order.OrderID), data); ok {
					data.Close()
					old.(*orderData).websocketUpdate(serverTime, order)
				}
			}
		}
	}
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

func (c *Client) GetOrder(ctx context.Context, orderID exchange.OrderID) (*exchange.Order, error) {
	resp, err := c.getOrder(ctx, string(orderID))
	if err != nil {
		return nil, fmt.Errorf("could not get order %s: %w", orderID, err)
	}
	return toExchangeOrder(&resp.Order), nil
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

func (c *Client) recreateOldOrder(clientOrderID string) (string, bool) {
	var old *OrderType
	if v, ok := c.oldFilled[clientOrderID]; ok {
		old = v
	} else if v, ok := c.oldCancelled[clientOrderID]; ok {
		old = v
	}
	if old == nil {
		return "", false
	}

	data := c.newOrderData(old.OrderID)
	if old, ok := c.orderDataMap.LoadOrStore(old.OrderID, data); ok {
		data.Close()
		data = old.(*orderData)
	}
	data.topic.SendCh() <- toExchangeOrder(old)
	return old.OrderID, true
}

func (c *Client) createOrder(ctx context.Context, request *CreateOrderRequest) (*CreateOrderResponse, error) {
	var old *OrderType
	if v, ok := c.oldFilled[request.ClientOrderID]; ok {
		old = v
	} else if v, ok := c.oldCancelled[request.ClientOrderID]; ok {
		old = v
	}
	if old != nil {
		data := c.newOrderData(old.OrderID)
		if old, ok := c.orderDataMap.LoadOrStore(old.OrderID, data); ok {
			data.Close()
			data = old.(*orderData)
		}
		data.topic.SendCh() <- toExchangeOrder(old)
		return &CreateOrderResponse{OrderID: old.OrderID, Success: true}, nil
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

	if resp.Success {
		data := c.newOrderData(resp.OrderID)
		if old, ok := c.orderDataMap.LoadOrStore(resp.OrderID, data); ok {
			data.Close()
			data = old.(*orderData)
		}
		// We need to wait here to confirm the order because orders need sometime
		// before they can be canceled by their ids.
		if _, err := data.waitForOpen(ctx); err != nil {
			return nil, err
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
	at := fmt.Sprintf("%d", c.now().Unix())
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

	at := fmt.Sprintf("%d", c.now().Unix())
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
