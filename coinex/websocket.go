// Copyright (c) 2025 BVK Chaitanya

package coinex

import (
	"bytes"
	"cmp"
	"compress/gzip"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"log/slog"
	"os"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/bvk/tradebot/coinex/internal"
	"github.com/bvk/tradebot/exchange"
	"github.com/bvk/tradebot/syncmap"

	"github.com/gorilla/websocket"
)

func (c *Client) goGetMessages(ctx context.Context) {
	defer c.wg.Done()

	for i := 0; ctx.Err() == nil; i = max(i+1, 5) {
		if err := c.getMessages(ctx); err != nil {
			if !errors.Is(err, os.ErrClosed) {
				slog.Warn("could not get messages over websocket (may retry)", "err", err)
			}
			// FIXME: Following needs reset logic as well.
			if err := sleep(ctx, time.Second<<i); err != nil {
				return
			}
		}
	}
}

func (c *Client) getMessages(ctx context.Context) (status error) {
	// Reinitialize the websocket call map.
	c.websocketCallMap = syncmap.Map[int64, *internal.WebsocketCall]{}
	defer func() {
		// Cancel all existing calls with an error.
		for _, call := range c.websocketCallMap.Range {
			if status != nil {
				call.Status = status
			} else {
				call.Status = os.ErrClosed
			}
			close(call.DoneCh)
		}
	}()

	var wg sync.WaitGroup
	wg.Wait()

	ctx, cancel := context.WithCancelCause(ctx)
	defer func() {
		if status != nil {
			cancel(status)
		} else {
			cancel(os.ErrClosed)
		}
	}()

	// Open a new websocket connection.
	dialer := websocket.Dialer{
		EnableCompression: true,
	}
	conn, _, err := dialer.DialContext(ctx, WebsocketURL.String(), nil)
	if err != nil {
		slog.Error("could not dial to websocket feed", "err", err)
		return err
	}
	defer conn.Close()

	// Start a message reader in the background.
	wg.Add(1)
	go func() {
		defer wg.Done()

		for ctx.Err() == nil {
			msg, err := c.readMessage(ctx, conn)
			if err != nil {
				if !errors.Is(err, os.ErrClosed) {
					slog.Error("could not read websocket message", "err", err)
				}
				cancel(err)
				return
			}
			if err := c.handleMessage(ctx, msg); err != nil {
				slog.Error("could not handle websocket message", "err", err)
				continue
			}
		}
	}()

	// Start a message writer in the background.
	wg.Add(1)
	go func() {
		defer wg.Done()

		id := int64(0)
		for ctx.Err() == nil {
			select {
			case <-ctx.Done():
				return

			case call := <-c.websocketCallCh:
				call.Request.ID = id + 1
				id++
				c.websocketCallMap.Store(id, call)

				if err := conn.WriteJSON(&call.Request); err != nil {
					slog.Error("could not send websocket request", "method", call.Request.Method, "err", err)
					cancel(err)
					return
				}
			}
		}
	}()

	if err := c.websocketPing(ctx); err != nil {
		return err
	}

	// Resend a sign message, resubscribe to all channels, etc. and send ping
	// messages periodically to keep the websocket alive.
	if err := c.websocketSign(ctx); err != nil {
		return err
	}

	// Subscribe for state.update messages on markets.
	var markets []string
	for m, _ := range c.marketDealUpdateMap.Range {
		markets = append(markets, m)
	}
	if len(markets) > 0 {
		if err := c.websocketMarketListSubscribe(ctx, "deals.subscribe", markets); err != nil {
			slog.Error("could not resubscribe for market deal updates", "markets", markets, "err", err)
			return err
		}
		if err := c.websocketMarketListSubscribe(ctx, "order.subscribe", markets); err != nil {
			return err
		}
	}

	for ctx.Err() == nil {
		if err := c.websocketPing(ctx); err != nil {
			slog.Error("websocket ping failed; reopening new socket", "err", err)
			return err
		}
		if err := sleep(ctx, time.Minute); err != nil {
			return err
		}
	}

	return context.Cause(ctx)
}

func (c *Client) readMessage(ctx context.Context, conn *websocket.Conn) (json.RawMessage, error) {
	stopc := make(chan struct{})
	stop := context.AfterFunc(ctx, func() {
		conn.SetReadDeadline(time.Now())
		close(stopc)
	})

	_, msg, err := conn.ReadMessage()
	if !stop() {
		// The AfterFunc was started. Wait for it to complete, and reset the Conn's
		// deadline.
		<-stopc
		conn.SetReadDeadline(time.Time{})
		return nil, context.Cause(ctx)
	}
	if err != nil {
		slog.Error("could not read websocket message", "err", err)
		return nil, err
	}

	// HACK HACK HACK: Identify compressed messages and uncompress them forcibly.
	if msg[0] == 0x1f && msg[1] == 0x8b {
		reader, err := gzip.NewReader(bytes.NewReader(msg))
		if err != nil {
			slog.Error("could not create gzip reader", "err", err)
			return nil, err
		}
		data, err := io.ReadAll(reader)
		if err != nil {
			slog.Error("could not uncompress with gzip reader", "err", err)
			return nil, err
		}
		msg = data
	}

	var m json.RawMessage
	if err := json.Unmarshal(msg, &m); err != nil {
		log.Printf("message=%s", msg)
		slog.Error("could not Unmarshal websocket message", "err", err)
		return nil, err
	}
	return m, nil
}

func (c *Client) handleMessage(ctx context.Context, msg json.RawMessage) error {
	var header internal.WebsocketHeader
	if err := json.Unmarshal([]byte(msg), &header); err != nil {
		slog.Error("could not unmarshal webosocket message header", "msg", string(msg), "err", err)
		return err
	}

	switch {
	case header.IsRequest():
		return fmt.Errorf("incoming websocket requests are not supported")

	case header.IsResponse():
		call, ok := c.websocketCallMap.LoadAndDelete(*header.ID)
		if !ok {
			slog.Warn("could not find websocket call with incoming id (ignored)", "id", *header.ID, "msg", string(msg))
			return nil
		}
		if err := json.Unmarshal([]byte(msg), &call.Response); err != nil {
			slog.Error("could not unmarshal websocket response", "msg", string(msg), "err", err)
			call.Status = err
			close(call.DoneCh)
			return err
		}
		close(call.DoneCh)

	case header.IsNotice():
		handler, ok := c.websocketHandlerMap[*header.Method]
		if !ok {
			slog.Warn("could not find notice handler for incoming method (ignored)", "method", *header.Method, "msg", string(msg))
			return nil
		}
		notice := new(internal.WebsocketNotice)
		if err := json.Unmarshal([]byte(msg), notice); err != nil {
			slog.Error("could not unmarshal weboscket notice", "msg", string(msg), "err", err)
			return err
		}
		return handler(ctx, notice)

	default:
		return fmt.Errorf("could not identify websocket message type")
	}

	return nil
}

func (c *Client) websocketCall(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
	call := internal.WebsocketCall{
		DoneCh: make(chan struct{}),
		Request: internal.WebsocketRequest{
			Method: method,
			Params: params,
		},
	}
	// Send request.
	select {
	case <-ctx.Done():
		return nil, context.Cause(ctx)
	case c.websocketCallCh <- &call:
	}
	// Receive response.
	select {
	case <-ctx.Done():
		return nil, context.Cause(ctx)
	case <-call.DoneCh:
		if call.Status != nil {
			return nil, call.Status
		}
		if call.Response.Code != 0 {
			return nil, fmt.Errorf("method %q failed: code=%d message=%q", method, call.Response.Code, call.Response.Message)
		}
		// log.Printf("call=%s input=%s output=%s", method, call.Request, call.Response.Data)
		return call.Response.Data, nil
	}
}

func (c *Client) websocketPing(ctx context.Context) error {
	resp, err := c.websocketCall(ctx, "server.ping", json.RawMessage("{}"))
	if err != nil {
		slog.Error("could not perform websocket ping", "err", err)
		return err
	}
	log.Printf("ping-response: %s", resp)
	return nil
}

func (c *Client) websocketServerTime(ctx context.Context) (exchange.RemoteTime, error) {
	var zero exchange.RemoteTime
	jsresp, err := c.websocketCall(ctx, "server.time", json.RawMessage("{}"))
	if err != nil {
		slog.Error("could not perform websocket ping", "err", err)
		return zero, err
	}
	log.Printf("server-time-response: %s", jsresp)
	type ServerTime struct {
		Timestamp int64 `json:"timestamp"`
	}
	resp := new(ServerTime)
	if err := json.Unmarshal([]byte(jsresp), &resp); err != nil {
		return zero, err
	}
	return exchange.RemoteTime{Time: time.UnixMilli(resp.Timestamp)}, nil
}

func (c *Client) websocketSign(ctx context.Context) error {
	type Params struct {
		Key       string `json:"access_id"`
		Signature string `json:"signed_str"`
		Timestamp int64  `json:"timestamp"`
	}

	now := time.Now().UnixMilli()
	timestamp := strconv.FormatInt(now, 10)
	hash := hmac.New(sha256.New, []byte(c.secret))
	io.WriteString(hash, timestamp)
	signature := hash.Sum(nil)

	p := &Params{
		Key:       c.key,
		Timestamp: now,
		Signature: hex.EncodeToString(signature),
	}
	params, err := json.Marshal(p)
	if err != nil {
		return err
	}
	jsresp, err := c.websocketCall(ctx, "server.sign", json.RawMessage(params))
	if err != nil {
		slog.Error("could not authenticate with websocket", "err", err)
		return err
	}
	log.Printf("sign-response: %s", jsresp)
	return nil
}

func (c *Client) websocketMarketListSubscribe(ctx context.Context, method string, markets []string) error {
	type Params struct {
		MarketList []string `json:"market_list"`
	}
	p := &Params{
		MarketList: markets,
	}
	params, err := json.Marshal(p)
	if err != nil {
		return err
	}

	if resp, err := c.websocketCall(ctx, method, params); err != nil {
		log.Printf("subscribe with market list request failed: response=%s", resp)
		slog.Error("could not subscribe with market list", "method", method, "markets", markets)
		return err
	}
	return nil
}

func (c *Client) websocketMarketListUnsubscribe(ctx context.Context, method string, markets []string) error {
	type Params struct {
		MarketList []string `json:"market_list"`
	}
	p := &Params{
		MarketList: markets,
	}
	params, err := json.Marshal(p)
	if err != nil {
		return err
	}

	if resp, err := c.websocketCall(ctx, method, params); err != nil {
		log.Printf("unsubscribe with market list request failed: response=%s", resp)
		slog.Error("could not unsubscribe with market list", "method", method, "markets", markets)
		return err
	}
	return nil
}

func (c *Client) onDealUpdate(ctx context.Context, notice *internal.WebsocketNotice) error {
	type Data struct {
		Market   string                 `json:"market"`
		DealList []*internal.DealUpdate `json:"deal_list"`
	}
	var data Data
	if err := json.Unmarshal([]byte(notice.Data), &data); err != nil {
		slog.Error("could not unmarshal deal.update data", "err", err)
		log.Printf("deal.update notice data=%s", notice.Data)
		return err
	}

	latest := slices.MaxFunc(data.DealList, func(a, b *internal.DealUpdate) int {
		return cmp.Compare(a.CreatedAt, b.CreatedAt)
	})

	if topic, ok := c.marketDealUpdateMap.Load(data.Market); ok {
		topic.Send(latest)
	}
	return nil
}

func (c *Client) onOrderUpdate(ctx context.Context, notice *internal.WebsocketNotice) error {
	update := new(internal.OrderUpdate)
	if err := json.Unmarshal([]byte(notice.Data), &update); err != nil {
		slog.Error("could not unmarshal order.update data", "err", err)
		log.Printf("order.update notice data=%s", notice.Data)
		return err
	}
	if update.Event == "finish" {
		update.Order.HasFinishEvent = true
	}

	c.getMarketOrdersTopic(update.Order.Market).Send(update.Order)
	if update.Order.HasFinishEvent && !update.Order.FilledAmount.IsZero() {
		c.refreshOrdersTopic.Send(update.Order)
	}
	return nil
}
