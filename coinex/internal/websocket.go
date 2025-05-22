// Copyright (c) 2025 BVK Chaitanya

package internal

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/bvk/tradebot/exchange"
	"github.com/bvk/tradebot/syncmap"
	"github.com/gorilla/websocket"
)

func (c *Client) goGetMessages(ctx context.Context) {
	defer c.wg.Done()

	for i := 0; ctx.Err() == nil; i = max(i+1, 5) {
		if err := c.getMessages(ctx); err != nil {
			slog.Warn("could not get messages over websocket (may retry)", "err", err)
			if err := sleep(ctx, time.Second<<i); err != nil {
				return
			}
		}
	}
}

func (c *Client) getMessages(ctx context.Context) (status error) {
	// Reinitialize the websocket call map.
	c.websocketCallMap = syncmap.Map[int64, *websocketCall]{}
	defer func() {
		// Cancel all existing calls with an error.
		for _, call := range c.websocketCallMap.Range {
			if status != nil {
				call.status = status
			} else {
				call.status = os.ErrClosed
			}
			close(call.doneCh)
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
				slog.Error("could not read websocket message", "err", err)
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

	// Resend a sign message, resubscribe to all channels, etc. and send ping
	// messages periodically to keep the websocket alive.
	if err := c.websocketSign(ctx); err != nil {
		return err
	}

	// TODO: Re-subscribe to all channels.

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
	var header WebsocketHeader
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
			call.status = err
			close(call.doneCh)
			return err
		}
		close(call.doneCh)

	case header.IsNotice():
		handler, ok := c.websocketHandlerMap[*header.Method]
		if !ok {
			slog.Warn("could not find notice handler for incoming method (ignored)", "method", *header.Method, "msg", string(msg))
			return nil
		}
		notice := new(WebsocketNotice)
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
	call := websocketCall{
		doneCh: make(chan struct{}),
		Request: WebsocketRequest{
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
	case <-call.doneCh:
		if call.status != nil {
			return nil, call.status
		}
		if call.Response.Code != 0 {
			return nil, fmt.Errorf("method %q failed: code=%d message=%q", method, call.Response.Code, call.Response.Message)
		}
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
	return nil // TODO
}

// func (c *Client) Subscribe(ctx context.Context, market string) error {
// 	type Params struct {
// 		MarketList []string `json:"market_list"`
// 	}
// 	p := &Params{
// 		MarketList: []string{market},
// 	}
// 	jsdata, err := json.Marshal(p)
// 	if err != nil {
// 		return err
// 	}

// 	msg := &WriteMessage{
// 		Method: "index.subscribe",
// 		ID:     1,
// 		Params: json.RawMessage(jsdata),
// 	}
// 	select {
// 	case <-ctx.Done():
// 		return context.Cause(ctx)
// 	case c.websocketSendCh <- msg:
// 		return nil
// 	}
// }

// func (c *Client) Unsubscribe(ctx context.Context, market string) error {
// 	type Params struct {
// 		MarketList []string `json:"market_list"`
// 	}
// 	p := &Params{
// 		MarketList: []string{market},
// 	}
// 	jsdata, err := json.Marshal(p)
// 	if err != nil {
// 		return err
// 	}

// 	msg := &WriteMessage{
// 		Method: "index.unsubscribe",
// 		ID:     1,
// 		Params: json.RawMessage(jsdata),
// 	}
// 	select {
// 	case <-ctx.Done():
// 		return context.Cause(ctx)
// 	case c.websocketSendCh <- msg:
// 		return nil
// 	}
// }

func (c *Client) onIndexUpdate(ctx context.Context, notice *WebsocketNotice) error {
	log.Printf("method=%s data=%s", notice.Method, notice.Data)
	return nil
}
