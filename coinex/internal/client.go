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
	"sync"
	"time"

	"github.com/bvk/tradebot/syncmap"

	"github.com/bvkgo/topic"
)

type WebsocketNoticeHandler func(context.Context, *WebsocketNotice) error

type Client struct {
	lifeCtx    context.Context
	lifeCancel context.CancelCauseFunc

	wg sync.WaitGroup

	opts Options

	client http.Client

	key, secret string

	marketDealUpdateMap syncmap.Map[string, *topic.Topic[*DealUpdate]]

	websocketHandlerMap map[string]WebsocketNoticeHandler

	websocketCallCh  chan *websocketCall
	websocketCallMap syncmap.Map[int64, *websocketCall]
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
		websocketHandlerMap: make(map[string]WebsocketNoticeHandler),
		websocketCallCh:     make(chan *websocketCall, 10),
	}
	c.websocketHandlerMap["deals.update"] = c.onDealUpdate

	c.wg.Add(1)
	go c.goGetMessages(c.lifeCtx)
	return c, nil
}

// Close releases resources and destroys the client instance.
func (c *Client) Close() error {
	c.lifeCancel(os.ErrClosed)
	c.wg.Wait()
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

func (c *Client) GetMarket(ctx context.Context, market string) (*GetMarketsResponse, error) {
	values := make(url.Values)
	values.Set("market", market)

	addrURL := &url.URL{
		Scheme:   RestURL.Scheme,
		Host:     RestURL.Host,
		Path:     path.Join(RestURL.Path, "/spot/market"),
		RawQuery: values.Encode(),
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

// GetBalances retrieves all funds information in spot accounts.
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

func (c *Client) CreateOrder(ctx context.Context, req *CreateOrderRequest) (*CreateOrderResponse, error) {
	addrURL := &url.URL{
		Scheme: RestURL.Scheme,
		Host:   RestURL.Host,
		Path:   path.Join(RestURL.Path, "/spot/order"),
	}
	resp := new(CreateOrderResponse)
	if err := privatePostJSON(ctx, c, addrURL, req /* request */, resp); err != nil {
		if !errors.Is(err, context.Canceled) {
			slog.Error("could not create order", "market", req.Market, "side", req.Side, "price", req.Price, "size", req.Amount, "url", addrURL, "err", err)
		}
		return nil, err
	}
	return resp, nil
}

func (c *Client) GetOrder(ctx context.Context, market string, orderID int64) (*GetOrderResponse, error) {
	values := make(url.Values)
	values.Set("market", market)
	values.Set("order_id", strconv.FormatInt(orderID, 10))

	addrURL := &url.URL{
		Scheme:   RestURL.Scheme,
		Host:     RestURL.Host,
		Path:     path.Join(RestURL.Path, "/spot/order-status"),
		RawQuery: values.Encode(),
	}
	resp := new(GetOrderResponse)
	if err := privateGetJSON(ctx, c, addrURL, nil /* request */, resp); err != nil {
		if !errors.Is(err, context.Canceled) {
			slog.Error("could not get market ticker information", "url", addrURL, "err", err)
		}
		return nil, err
	}
	return resp, nil
}

func (c *Client) CancelOrder(ctx context.Context, market, marketType string, orderID int64) (*CancelOrderResponse, error) {
	req := CancelOrderRequest{
		Market:     market,
		MarketType: marketType,
		OrderID:    orderID,
	}
	addrURL := &url.URL{
		Scheme: RestURL.Scheme,
		Host:   RestURL.Host,
		Path:   path.Join(RestURL.Path, "/spot/cancel-order"),
	}
	resp := new(CancelOrderResponse)
	if err := privatePostJSON(ctx, c, addrURL, &req, resp); err != nil {
		if !errors.Is(err, context.Canceled) {
			slog.Error("could not cancel order", "orderID", orderID, "market", market, "url", addrURL, "err", err)
		}
		return nil, err
	}
	return resp, nil
}

// WatchMarket subscribes to streaming updates for a market.
func (c *Client) WatchMarket(ctx context.Context, market string) error {
	if _, ok := c.marketDealUpdateMap.Load(market); ok {
		return os.ErrExist
	}
	if err := c.websocketMarketListSubscribe(ctx, "deals.subscribe", []string{market}); err != nil {
		return err
	}
	c.marketDealUpdateMap.LoadOrStore(market, topic.New[*DealUpdate]())
	return nil
}

// UnwatchMarket unsubscribes from streaming updates for a market.
func (c *Client) UnwatchMarket(ctx context.Context, market string) error {
	old, ok := c.marketDealUpdateMap.Load(market)
	if !ok {
		return os.ErrNotExist
	}
	if err := c.websocketMarketListUnsubscribe(ctx, "deals.unsubscribe", []string{market}); err != nil {
		return err
	}
	if ok := c.marketDealUpdateMap.CompareAndDelete(market, old); ok {
		old.Close()
	}
	return nil
}

func (c *Client) do(ctx context.Context, method string, addrURL *url.URL, body, contentType string) (*http.Response, error) {
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

	if len(contentType) != 0 {
		req.Header.Add("Content-Type", contentType)
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
	contentType := ""
	if request != nil {
		if err := json.NewEncoder(&sb).Encode(request); err != nil {
			return err
		}
		contentType = "application/json"
	}

	resp, err := c.do(ctx, http.MethodGet, addrURL, sb.String(), contentType)
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

func privatePostJSON[PT *T, T any](ctx context.Context, c *Client, addrURL *url.URL, request any, response PT) error {
	var sb strings.Builder
	if request != nil {
		if err := json.NewEncoder(&sb).Encode(request); err != nil {
			return err
		}
	}

	resp, err := c.do(ctx, http.MethodPost, addrURL, sb.String(), "application/json")
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

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	// Parse into a generic response.
	var genericResp GenericResponse
	if err := json.Unmarshal(data, &genericResp); err != nil {
		slog.Error("could not unmarshal into generic response", "response", string(data), "err", err)
		return err
	}
	if genericResp.Code != 0 {
		slog.Error("POST request failed", "url", addrURL, "body", sb.String(), "response", string(data), "err", err)
		return fmt.Errorf("failed with code=%d message=%s", genericResp.Code, genericResp.Message)
	}

	if err := json.Unmarshal(data, response); err != nil {
		slog.Error("could not decode response to json", "err", err)
		return err
	}
	return nil
}
