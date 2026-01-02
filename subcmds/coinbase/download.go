// Copyright (c) 2026 BVK Chaitanya

package coinbase

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
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

	stopTime string
}

func (c *Download) Command() (string, *flag.FlagSet, cli.CmdFunc) {
	fset := new(flag.FlagSet)
	fset.StringVar(&c.secretsPath, "secrets-file", "", "path to credentials file")
	fset.StringVar(&c.outputPath, "output-file", "", "path to output file")
	fset.StringVar(&c.stopTime, "stop-time", "", "Oldest date/time to stop the download")
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
	var stopTime time.Time
	if len(c.stopTime) != 0 {
		v, err := parseTime(c.stopTime)
		if err != nil {
			return err
		}
		stopTime = v
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
			resp.Body.Close()
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
		if err := c.dropAfterStopTime(reply, stopTime); err != nil {
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

func (c *Download) dropAfterStopTime(resp *ListOrdersResponse, stopTime time.Time) error {
	if stopTime.IsZero() {
		return nil
	}
	type Order struct {
		LastFillTime exchange.RemoteTime `json:"last_fill_time"`
	}
	for i, v := range resp.Orders {
		order := new(Order)
		if err := json.Unmarshal([]byte(v), order); err != nil {
			return err
		}
		log.Println(order.LastFillTime.Time)
		if order.LastFillTime.Time.Before(stopTime) {
			resp.Orders = resp.Orders[:i]
			resp.HasNext = false
			return nil
		}
	}
	return nil
}
