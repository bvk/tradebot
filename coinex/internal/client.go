// Copyright (c) 2025 BVK Chaitanya

package internal

import (
	"context"
	"os"
)

type Client struct {
	lifeCtx    context.Context
	lifeCancel context.CancelCauseFunc
}

// New returns a new client instance.
func New() (*Client, error) {
	lifeCtx, lifeCancel := context.WithCancelCause(context.Background())
	c := &Client{
		lifeCtx:    lifeCtx,
		lifeCancel: lifeCancel,
	}
	return c, nil
}

// Close releases resources and destroys the client instance.
func (c *Client) Close() error {
	c.lifeCancel(os.ErrClosed)
	return nil
}
