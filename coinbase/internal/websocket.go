// Copyright (c) 2023 BVK Chaitanya

package internal

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"log/slog"
	"slices"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bvk/tradebot/ctxutil"
	"github.com/bvk/tradebot/exchange"
	"github.com/gorilla/websocket"
)

type Message struct {
	Type string `json:"type"`

	// Message holds description when Type is "error".
	Message string `json:"message"`

	ProductIDs []string `json:"product_ids"`
	Channel    string   `json:"channel"`
	APIKey     string   `json:"api_key"`
	Timestamp  string   `json:"timestamp"`
	Signature  string   `json:"signature"`

	Sequence int64 `json:"sequence_num,number"`

	ClientID string  `json:"client_id"`
	Events   []Event `json:"events"`
}

type Event struct {
	Type      string         `json:"type"`
	ProductID string         `json:"product_id"`
	Updates   []*UpdateEvent `json:"updates"`
	Tickers   []*TickerEvent `json:"tickers"`
	Orders    []*OrderEvent  `json:"orders"`
}

type UpdateEvent struct {
	Side        string               `json:"side"`
	EventTime   string               `json:"event_time"`
	PriceLevel  exchange.NullDecimal `json:"price_level"`
	NewQuantity exchange.NullDecimal `json:"new_quantity"`
}

type TickerEvent struct {
	Type        string               `json:"type"`
	ProductID   string               `json:"product_id"`
	Price       exchange.NullDecimal `json:"price"`
	Volume24H   exchange.NullDecimal `json:"volume_24_h"`
	Low24H      exchange.NullDecimal `json:"low_24_h"`
	High24H     exchange.NullDecimal `json:"high_24_h"`
	Low52W      exchange.NullDecimal `json:"low_52_w"`
	High52W     exchange.NullDecimal `json:"high_52_w"`
	PricePct24H exchange.NullDecimal `json:"price_percent_chg_24_h"`
}

type OrderEvent struct {
	OrderID            string               `json:"order_id"`
	ClientOrderID      string               `json:"client_order_id"`
	Status             string               `json:"status"`
	ProductID          string               `json:"product_id"`
	CreatedTime        exchange.RemoteTime  `json:"creation_time"`
	OrderSide          string               `json:"order_side"`
	OrderType          string               `json:"order_type"`
	CancelReason       string               `json:"cancel_reason"`
	RejectReason       string               `json:"reject_reason"`
	CumulativeQuantity exchange.NullDecimal `json:"cumulative_quantity"`
	TotalFees          exchange.NullDecimal `json:"total_fees"`
	AvgPrice           exchange.NullDecimal `json:"avg_price"`
}

type Websocket struct {
	client *Client

	mu sync.Mutex

	dirty           atomic.Bool
	chanProductsMap map[string][]string
}

func (c *Client) newWebsocket() (_ *Websocket) {
	return &Websocket{
		client:          c,
		chanProductsMap: make(map[string][]string),
	}
}

func (w *Websocket) Close() {
	w.mu.Lock()
	w.chanProductsMap = make(map[string][]string)
	w.dirty.Store(true)
	w.mu.Unlock()
	// TODO: Wait for GetMessages to return
}

func (w *Websocket) dial(ctx context.Context) (*websocket.Conn, error) {
	var dialer websocket.Dialer
	conn, _, err := dialer.DialContext(ctx, "wss://"+w.client.opts.WebsocketHostname, nil)
	if err != nil {
		slog.ErrorContext(ctx, "could not dial to websocket feed", "error", err)
		return nil, err
	}
	return conn, nil
}

func (w *Websocket) Subscribe(channel string, products []string) {
	w.mu.Lock()
	defer w.mu.Unlock()

	dirty := false
	old := w.chanProductsMap[channel]
	nproducts := slices.Clone(old)
	for _, p := range products {
		if _, ok := slices.BinarySearch(old, p); !ok {
			dirty = true
			nproducts = append(nproducts, p)
		}
	}
	if dirty {
		sort.Strings(nproducts)
		w.chanProductsMap[channel] = nproducts
		w.dirty.Store(true)
	}
}

func (w *Websocket) Unsubscribe(channel string, products []string) {
	w.mu.Lock()
	defer w.mu.Unlock()

	dirty := false
	var nproducts []string
	old := w.chanProductsMap[channel]
	for _, p := range old {
		if slices.Contains(products, p) {
			dirty = true
			continue
		}
		nproducts = append(nproducts, p)
	}

	if dirty {
		sort.Strings(nproducts)
		w.chanProductsMap[channel] = nproducts
		w.dirty.Store(true)
	}
}

func (w *Websocket) diff(oldMap map[string][]string) (newMap, subMap, unsubMap map[string][]string) {
	w.mu.Lock()
	newMap = make(map[string][]string)
	for k, v := range w.chanProductsMap {
		newMap[k] = slices.Clone(v)
	}
	w.dirty.Store(false)
	w.mu.Unlock()

	// subSlice returns `a-b` as a slice, i.e., items present in `a`, but not in `b`.
	subSlice := func(a, b []string) []string {
		var vs []string
		for _, v := range a {
			if !slices.Contains(b, v) {
				vs = append(vs, v)
			}
		}
		return vs
	}

	// subKeys returns `a-b` as a map, i.e., keys present in `a`, but not in `b`.
	subKeys := func(a, b map[string][]string) map[string][]string {
		smap := make(map[string][]string)
		for k, v := range a {
			if _, ok := b[k]; !ok {
				smap[k] = v
			}
		}
		return smap
	}

	subMap = subKeys(newMap, oldMap)
	unsubMap = subKeys(oldMap, newMap)

	// Handle common items now.
	for k, old := range oldMap {
		if new, ok := newMap[k]; ok {
			if ps := subSlice(new, old); len(ps) > 0 {
				subMap[k] = ps
			}
			if ps := subSlice(old, new); len(ps) > 0 {
				unsubMap[k] = ps
			}
		}
	}

	return newMap, subMap, unsubMap
}

func readMessage(ctx context.Context, conn *websocket.Conn) (*Message, error) {
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
		return nil, err
	}

	m := new(Message)
	if err := json.Unmarshal(msg, m); err != nil {
		return nil, err
	}

	if strings.EqualFold(m.Channel, "user") {
		log.Printf("%s", msg)
	}

	if m.Type == "error" {
		log.Printf("received a websocket error message: %#v", *m)
		return nil, fmt.Errorf(m.Message)
	}
	return m, nil
}

func (c *Client) subscribeMsg(channel string, products []string) *Message {
	submsg := &Message{
		Type:       "subscribe",
		ProductIDs: products,
		Channel:    channel,
		APIKey:     c.key,
		Timestamp:  fmt.Sprintf("%d", c.Now().Unix()),
		Signature:  "",
	}
	subdata := fmt.Sprintf("%s%s%s", submsg.Timestamp, submsg.Channel, strings.Join(submsg.ProductIDs, ","))
	signature := c.sign(subdata)
	submsg.Signature = signature
	return submsg
}

func (c *Client) unsubscribeMsg(channel string, products []string) *Message {
	unsubmsg := &Message{
		Type:       "unsubscribe",
		ProductIDs: products,
		Channel:    channel,
		APIKey:     c.key,
		Timestamp:  fmt.Sprintf("%d", c.Now().Unix()),
		Signature:  "",
	}
	unsubdata := fmt.Sprintf("%s%s%s", unsubmsg.Timestamp, unsubmsg.Channel, strings.Join(unsubmsg.ProductIDs, ","))
	signature := c.sign(unsubdata)
	unsubmsg.Signature = signature
	return unsubmsg
}

type MessageHandler = func(*Message)

func (c *Client) GetMessages(channel string, products []string, handler MessageHandler) *Websocket {
	w := c.newWebsocket()
	w.Subscribe(channel, products)

	keys := func(m map[string][]string) (vs []string) {
		for k := range m {
			vs = append(vs, k)
		}
		return
	}

	dispatch := func(ctx context.Context) error {
		conn, err := w.dial(ctx)
		if err != nil {
			log.Printf("could not open new websocket (will retry): %v", err)
			return err
		}
		defer conn.Close()

		channels := []string{}
		chanProductsMap := make(map[string][]string)

		for ctx.Err() == nil {
			if w.dirty.Load() {
				clone, subs, unsubs := w.diff(chanProductsMap)
				for ch, ps := range unsubs {
					if err := conn.WriteJSON(w.client.unsubscribeMsg(ch, ps)); err != nil {
						log.Printf("could not unsubscribe %v products from channel %s: %v", ps, ch, err)
						return err
					}
					log.Printf("unsubscribed from channel %s for products %v", ch, ps)
				}
				for ch, ps := range subs {
					if err := conn.WriteJSON(w.client.subscribeMsg(ch, ps)); err != nil {
						log.Printf("could not subscribe to channel %s for products %v: %v", ch, ps, err)
						return err
					}
					log.Printf("subscribed to channel %s for products %v", ch, ps)
				}
				oldChannels := keys(chanProductsMap)
				chanProductsMap = clone
				if len(clone) == 0 {
					break
				}
				channels = keys(clone)
				log.Printf("websocket is updated to watch channels %v from previous %v", channels, oldChannels)
			}

			msg, err := readMessage(ctx, conn)
			if err != nil {
				if ctx.Err() == nil {
					log.Printf("closing the websocket connection to channels %v: %v", channels, err)
				}
				return err
			}
			handler(msg)
		}
		return context.Cause(ctx)
	}

	c.Go(func(ctx context.Context) {
		for ctx.Err() == nil {
			w.dirty.Store(true)
			if err := dispatch(ctx); err != nil && ctx.Err() == nil {
				ctxutil.Sleep(ctx, c.opts.WebsocketRetryInterval)
				continue
			}
			break
		}
	})

	return w
}
