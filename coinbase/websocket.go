// Copyright (c) 2023 BVK Chaitanya

package coinbase

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"slices"
	"strings"
	"time"

	ws "github.com/gorilla/websocket"
)

type Websocket struct {
	client *Client

	products []string
	conn     *ws.Conn

	channels []string
}

func (c *Client) NewWebsocket(ctx context.Context, products []string) (_ *Websocket, status error) {
	for _, p := range products {
		if !slices.Contains(c.spotProducts, p) {
			return nil, os.ErrInvalid
		}
	}

	var dialer ws.Dialer
	conn, _, err := dialer.DialContext(ctx, "wss://"+c.opts.WebsocketHostname, nil)
	if err != nil {
		slog.ErrorContext(ctx, "could not dial to websocket feed", "error", err)
		return nil, err
	}
	defer func() {
		if status != nil {
			conn.Close()
		}
	}()

	wbs := &Websocket{
		client:   c,
		products: products,
		conn:     conn,
	}
	if err := wbs.Subscribe(ctx, "heartbeats"); err != nil {
		slog.ErrorContext(ctx, "could not subscribe to heartbeats channel", "error", err)
		return nil, err
	}

	c.websockets = append(c.websockets, wbs)
	return wbs, nil
}

func (c *Client) CloseWebsocket(w *Websocket) error {
	index := slices.Index(w.client.websockets, w)
	if index < 0 {
		return os.ErrClosed
	}
	w.client.websockets = slices.Delete(w.client.websockets, index, index+1)

	// for len(w.channels) > 0 {
	// 	if err := w.Unsubscribe(w.client.ctx, w.channels[0]); err != nil {
	// 		slog.Error("could not close all channels", "channels", w.channels)
	// 		break
	// 	}
	// }

	_ = w.conn.Close()
	w.conn = nil
	return nil
}

func (w *Websocket) Subscribe(ctx context.Context, channel string) error {
	if w.conn == nil {
		return os.ErrClosed
	}
	if slices.Contains(w.channels, channel) {
		return os.ErrExist
	}

	submsg := MessageType{
		Type:       "subscribe",
		ProductIDs: w.products,
		Channel:    channel,
		APIKey:     w.client.key,
		Timestamp:  fmt.Sprintf("%d", w.client.now().Unix()),
		Signature:  "",
	}
	subdata := fmt.Sprintf("%s%s%s", submsg.Timestamp, submsg.Channel, strings.Join(submsg.ProductIDs, ","))
	signature := w.client.sign(subdata)
	submsg.Signature = signature

	if err := w.conn.WriteJSON(submsg); err != nil {
		slog.ErrorContext(ctx, "could not subscribe to the input channel", "channel", channel, "error", err)
		return err
	}
	w.channels = append(w.channels, channel)
	return nil
}

func (w *Websocket) Unsubscribe(ctx context.Context, channel string) error {
	if w.conn == nil {
		return os.ErrClosed
	}

	index := slices.Index(w.channels, channel)
	if index < 0 {
		slog.Error("websocket is not subscribed to input channel", "channel", channel)
		return os.ErrNotExist
	}
	w.channels = slices.Delete(w.channels, index, index+1)

	unsubmsg := MessageType{
		Type:       "unsubscribe",
		ProductIDs: w.products,
		Channel:    channel,
		APIKey:     w.client.key,
		Timestamp:  fmt.Sprintf("%d", w.client.now().Unix()),
		Signature:  "",
	}
	unsubdata := fmt.Sprintf("%s%s%s", unsubmsg.Timestamp, unsubmsg.Channel, strings.Join(unsubmsg.ProductIDs, ","))
	signature := w.client.sign(unsubdata)
	unsubmsg.Signature = signature

	if err := w.conn.WriteJSON(unsubmsg); err != nil {
		slog.ErrorContext(ctx, "could not unsubscribe from the input channel", "channel", channel, "error", err)
		return err
	}
	return nil
}

func (w *Websocket) NextMessage(ctx context.Context) (*MessageType, error) {
read:
	m, err := w.readMessage(ctx)
	if err != nil {
		return nil, err
	}
	if m.Type == "error" {
		return nil, fmt.Errorf(m.Message)
	}
	if m.Channel == "heartbeats" {
		goto read
	}
	return m, nil
}

func (w *Websocket) readMessage(ctx context.Context) (*MessageType, error) {
	nconn := w.conn.UnderlyingConn()
	stopc := make(chan struct{})
	stop := context.AfterFunc(ctx, func() {
		nconn.SetReadDeadline(time.Now())
		close(stopc)
	})

	_, msg, err := w.conn.ReadMessage()
	if !stop() {
		nconn.SetReadDeadline(time.Time{})
	}
	if err != nil {
		return nil, err
	}

	// Log only user channel messages. Skip pending status updates.
	if smsg := string(msg); strings.Contains(smsg, `"channel":"user"`) {
		if !strings.Contains(smsg, `"status":"PENDING"`) {
			slog.InfoContext(ctx, smsg)
		}
	}

	m := new(MessageType)
	if err := json.Unmarshal(msg, m); err != nil {
		return nil, err
	}
	return m, nil
}
