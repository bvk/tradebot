// Copyright (c) 2025 BVK Chaitanya

package internal

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bvk/tradebot/exchange"
)

type Client struct {
	lifeCtx    context.Context
	lifeCancel context.CancelCauseFunc

	wg sync.WaitGroup

	opts Options

	key, secret string

	client *http.Client

	// timeAdjustment is positive when local time is found to be ahead of the
	// server time, in which case, this value must be subtracted from the local
	// time before the local time can be used as a timestamp in the signature
	// calculations.
	timeAdjustment atomic.Int64
}

func New(ctx context.Context, key, secret string, opts *Options) (*Client, error) {
	if opts == nil {
		opts = new(Options)
	}
	if err := opts.Check(); err != nil {
		return nil, err
	}
	stime, err := getServerTime(ctx, opts)
	if err != nil {
		return nil, err
	}
	adjustment := time.Now().Sub(stime.Time)
	if adjustment >= opts.MaxTimeAdjustment {
		return nil, fmt.Errorf("local time is out-of-sync by %v with the server", adjustment)
	}

	jar, err := cookiejar.New(nil /* options */)
	if err != nil {
		slog.Error("could not create cookiejar", "err", err)
		return nil, fmt.Errorf("could not create cookiejar: %w", err)
	}

	lifeCtx, lifeCancel := context.WithCancelCause(context.Background())
	c := &Client{
		lifeCtx:    lifeCtx,
		lifeCancel: lifeCancel,

		opts:   *opts,
		key:    key,
		secret: secret,
		client: &http.Client{
			Jar:     jar,
			Timeout: opts.HttpClientTimeout,
		},
	}

	c.timeAdjustment.Store(int64(adjustment))
	c.wg.Add(1)
	go func() { c.goFindTimeAdjustment(c.lifeCtx); c.wg.Done() }()
	return c, nil
}

func (c *Client) Close() error {
	return nil
}

func (c *Client) goFindTimeAdjustment(ctx context.Context) {
	for {
		sctx, scancel := context.WithTimeout(ctx, time.Second)
		<-sctx.Done()
		scancel()

		if ctx.Err() != nil {
			return
		}

		stime, err := getServerTime(ctx, &c.opts)
		if err != nil {
			slog.Error("could not get server time (will retry)", "err", err)
			continue
		}

		diff := time.Now().Sub(stime.Time)
		c.timeAdjustment.Store(int64(diff))
		slog.Debug("updated local time adjustment", "adjustment", diff)
	}
}

// getServerTime returns exchange server's current time.
func getServerTime(ctx context.Context, opts *Options) (exchange.RemoteTime, error) {
	var zero exchange.RemoteTime

	addrURL := fmt.Sprintf("https://%s/sapi/v1/time", opts.RestHostname)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, addrURL, nil)
	if err != nil {
		return zero, err
	}
	client := http.Client{Timeout: opts.HttpClientTimeout}

	start := time.Now()
	resp, err := client.Do(req)
	stop := time.Now()
	if err != nil {
		return zero, err
	} else if resp.StatusCode != http.StatusOK {
		return zero, fmt.Errorf("get time failed with status code %d", resp.StatusCode)
	}
	defer resp.Body.Close()

	latency := stop.Sub(start)
	if latency > opts.MaxFetchTimeLatency {
		slog.Warn("get server time took too long", "latency", latency, "max-allowed", opts.MaxFetchTimeLatency)
		return zero, fmt.Errorf("get server time took too long (%v > %v)", latency, opts.MaxFetchTimeLatency)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		slog.Error("could not read server time response body", "err", err)
		return zero, err
	}

	type ServerTime struct {
		UnixMilli int64 `json:"serverTime"`
	}
	var st ServerTime
	if err := json.Unmarshal(body, &st); err != nil {
		return zero, err
	}
	stime := time.UnixMilli(st.UnixMilli).UTC()
	stime.Add(latency / 2)

	return exchange.RemoteTime{Time: stime}, nil
}
