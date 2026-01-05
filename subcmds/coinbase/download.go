// Copyright (c) 2026 BVK Chaitanya

package coinbase

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/bvk/tradebot/coinbase/advanced"
	"github.com/bvk/tradebot/exchange"
	"github.com/bvk/tradebot/server"
	"github.com/visvasity/cli"
)

type ListOrdersResponse struct {
	Orders   []json.RawMessage `json:"orders"`
	Sequence string            `json:"sequence,number"`
	Cursor   string            `json:"cursor"`
	HasNext  bool              `json:"has_next"`
}

type Download struct {
	secretsPath string

	outputPath string

	beginTime string
	endTime   string
}

func (c *Download) Command() (string, *flag.FlagSet, cli.CmdFunc) {
	fset := new(flag.FlagSet)
	fset.StringVar(&c.secretsPath, "secrets-file", "", "path to credentials file")
	fset.StringVar(&c.outputPath, "output-file", "", "path to output file")
	fset.StringVar(&c.beginTime, "begin-time", "", "Begin time for the orders time range")
	fset.StringVar(&c.endTime, "end-time", "", "End time for the orders time range")
	return "download", fset, cli.CmdFunc(c.run)
}

func (c *Download) run(ctx context.Context, args []string) error {
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	if len(c.outputPath) == 0 {
		return fmt.Errorf("output file is required")
	}
	if len(c.secretsPath) == 0 {
		return fmt.Errorf("secrets file is required")
	}

	now := time.Now()
	parseTime := func(s string) (time.Time, error) {
		if d, err := time.ParseDuration(s); err == nil {
			if d >= 0 {
				return time.Time{}, fmt.Errorf("stop-at as a duration must be a -ve value")
			}
			return now.Add(d), nil
		}
		if v, err := time.Parse("2006-01-02", s); err == nil {
			return v, nil
		}
		return time.Parse(time.RFC3339, s)
	}
	var beginTime time.Time
	if len(c.beginTime) != 0 {
		v, err := parseTime(c.beginTime)
		if err != nil {
			return err
		}
		beginTime = v
	}
	var endTime time.Time
	if len(c.endTime) != 0 {
		v, err := parseTime(c.endTime)
		if err != nil {
			return err
		}
		endTime = v
	}

	secrets, err := server.SecretsFromFile(c.secretsPath)
	if err != nil {
		return fmt.Errorf("could not load secrets: %w", err)
	}
	if secrets.Coinbase == nil {
		return fmt.Errorf("coinbase credentials are missing")
	}

	outp, err := os.Create(c.outputPath)
	if err != nil {
		return err
	}
	defer outp.Close()

	opts := &advanced.Options{
		HttpClientTimeout:   10 * time.Minute,
		MaxFetchTimeLatency: time.Second,
		MaxTimeAdjustment:   time.Second,
	}
	client, err := advanced.New(ctx, secrets.Coinbase.KID, secrets.Coinbase.PEM, opts)
	if err != nil {
		return fmt.Errorf("could not create coinbase client: %w", err)
	}
	defer client.Close()

	values := make(url.Values)
	values.Add("limit", "100")
	values.Add("sort_by", "LAST_FILL_TIME")
	if !endTime.IsZero() {
		values.Add("end_date", endTime.Format(time.RFC3339))
	}

	url := &url.URL{
		Scheme: "https",
		Host:   opts.RestHostname,
		Path:   "/api/v3/brokerage/orders/historical/batch",
	}
	for ctx.Err() == nil {
		url.RawQuery = values.Encode()
		resp, err := client.Do(ctx, http.MethodGet, url, nil)
		if err != nil {
			slog.Error("could not list orders", "url", url.String(), "err", err)
			return err
		}
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if len(body) != 0 {
				slog.Error("response", "body", string(body))
			}
			return fmt.Errorf("received non-ok status code %d", resp.StatusCode)
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return err
		}
		reply := new(ListOrdersResponse)
		if err := json.Unmarshal(body, reply); err != nil {
			return err
		}
		if err := c.truncate(reply, beginTime, endTime); err != nil {
			return err
		}
		for _, order := range reply.Orders {
			outp.Write(order)
			outp.Write([]byte("\n"))
		}
		if !reply.HasNext {
			break
		}
		values.Set("cursor", reply.Cursor)
		if n := len(reply.Orders); n > 0 {
			last := reply.Orders[n-1]
			fmt.Println(string(last))
		}
	}

	return nil
}

func (c *Download) truncate(resp *ListOrdersResponse, beginTime, endTime time.Time) error {
	if beginTime.IsZero() && endTime.IsZero() {
		return nil
	}
	type Order struct {
		LastFillTime exchange.RemoteTime `json:"last_fill_time"`
	}
	ncut := 0
	for i, v := range resp.Orders {
		order := new(Order)
		if err := json.Unmarshal([]byte(v), order); err != nil {
			return err
		}
		if !endTime.IsZero() && (order.LastFillTime.Time.Equal(endTime) || order.LastFillTime.Time.After(endTime)) {
			ncut++
			continue
		}
		if !beginTime.IsZero() && order.LastFillTime.Time.Before(beginTime) {
			resp.Orders = resp.Orders[:i]
			resp.HasNext = false
			break
		}
	}
	if ncut > 0 {
		resp.Orders = resp.Orders[ncut:]
	}
	return nil
}
