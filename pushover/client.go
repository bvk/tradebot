// Copyright (c) 2023 BVK Chaitanya

package pushover

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

type Client struct {
	token      string
	user       string
	httpClient *http.Client
}

func New(keys *Keys) (*Client, error) {
	c := &Client{
		token:      keys.ApplicationKey,
		user:       keys.UserKey,
		httpClient: &http.Client{},
	}
	return c, nil
}

func (c *Client) SendMessage(ctx context.Context, at time.Time, msg string) error {
	type Message struct {
		Token     string `json:"token"`
		User      string `json:"user"`
		Message   string `json:"message"`
		Timestamp int64  `json:"timestamp"`
	}
	m := &Message{
		Token:     c.token,
		User:      c.user,
		Timestamp: at.Unix(),
		Message:   msg,
	}
	var msgbuf bytes.Buffer
	if err := json.NewEncoder(&msgbuf).Encode(m); err != nil {
		return fmt.Errorf("could not json-encode message: %w", err)
	}
	u := &url.URL{
		Scheme: "https",
		Host:   "api.pushover.net",
		Path:   "/1/messages.json",
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), &msgbuf)
	if err != nil {
		return fmt.Errorf("could not create post request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("could not perform post request: %w", err)
	}
	defer resp.Body.Close()
	type Response struct {
		Status  int      `json:"status"`
		Request string   `json:"request"`
		Errors  []string `json:"errors"`
	}
	r := new(Response)
	if err := json.NewDecoder(resp.Body).Decode(r); err != nil {
		return fmt.Errorf("could not json-decode response for http-status %d: %w", resp.StatusCode, err)
	}
	if r.Status != 1 {
		if len(r.Errors) != 0 {
			return fmt.Errorf("send failed with http-status %d and error: %w", resp.StatusCode, errors.New(r.Errors[0]))
		}
		return fmt.Errorf("send failed with http-status %d and zero response-status code (%#v)", resp.StatusCode, *r)
	}
	return nil
}
