// Copyright (c) 2023 BVK Chaitanya

package coinbase

import (
	"context"
	"fmt"
	"net/url"
	"path"
	"time"

	"github.com/bvk/tradebot/gobs"
)

type CandleGranularity time.Duration

const (
	OneMinuteCandle     = CandleGranularity(time.Minute)
	FiveMinuteCandle    = CandleGranularity(5 * time.Minute)
	FifteenMinuteCandle = CandleGranularity(15 * time.Minute)
	ThirtyMinuteCandle  = CandleGranularity(30 * time.Minute)
	OneHourCandle       = CandleGranularity(time.Hour)
	TwoHourCandle       = CandleGranularity(2 * time.Hour)
	SixHourCandle       = CandleGranularity(6 * time.Hour)
	OneDayCandle        = CandleGranularity(24 * time.Hour)
)

func (c *Client) getProductCandles(ctx context.Context, productID string, from time.Time, granularity CandleGranularity) (*GetProductCandlesResponse, error) {
	end := from.Add(300 * time.Duration(granularity))

	g := ""
	switch granularity {
	case OneMinuteCandle:
		g = "ONE_MINUTE"
	case FiveMinuteCandle:
		g = "FIVE_MINUTE"
	case FifteenMinuteCandle:
		g = "FIFTEEN_MINUTE"
	case ThirtyMinuteCandle:
		g = "THIRTY_MINUTE"
	case OneHourCandle:
		g = "ONE_HOUR"
	case TwoHourCandle:
		g = "TWO_HOUR"
	case SixHourCandle:
		g = "SIX_HOUR"
	case OneDayCandle:
		g = "ONE_DAY"
	}

	values := make(url.Values)
	values.Set("start", fmt.Sprintf("%d", from.Unix()))
	values.Set("end", fmt.Sprintf("%d", end.Unix()))
	values.Set("granularity", g)

	url := &url.URL{
		Scheme:   "https",
		Host:     c.opts.RestHostname,
		Path:     path.Join("/api/v3/brokerage/products/", productID, "/candles"),
		RawQuery: values.Encode(),
	}
	resp := new(GetProductCandlesResponse)
	if err := c.httpGetJSON(ctx, url, resp); err != nil {
		return nil, fmt.Errorf("could not http-get product candles %q: %w", productID, err)
	}
	return resp, nil
}

func (c *Client) GetHourCandles(ctx context.Context, productID string, from time.Time) ([]*gobs.Candle, error) {
	resp, err := c.getProductCandles(ctx, productID, from, OneHourCandle)
	if err != nil {
		return nil, err
	}
	var cs []*gobs.Candle
	for _, c := range resp.Candles {
		start := time.Unix(c.Start, 0).UTC()
		gc := &gobs.Candle{
			StartTime: gobs.RemoteTime{Time: start},
			Low:       c.Low,
			High:      c.High,
			Open:      c.Open,
			Close:     c.Close,
			Volume:    c.Volume,
		}
		gc.EndTime = gobs.RemoteTime{Time: start.Add(time.Hour)}
		cs = append(cs, gc)
	}
	return cs, nil
}
