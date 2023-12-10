// Copyright (c) 2023 BVK Chaitanya

package exchange

import (
	"context"
	"fmt"
	"path"
	"time"

	"github.com/bvk/tradebot/gobs"
	"github.com/bvk/tradebot/kvutil"
	"github.com/bvk/tradebot/server"
	"github.com/bvkgo/kv"
	"github.com/shopspring/decimal"
)

func LoadCandles(ctx context.Context, db kv.Database, exchange, product string, day time.Time) ([]*gobs.Candle, error) {
	day = day.UTC()
	key := path.Join(server.CandlesKeyspace, exchange, product, day.Format("2006-01-02"))

	gcs, err := kvutil.GetDB[gobs.Candles](ctx, db, key)
	if err != nil {
		return nil, fmt.Errorf("could not load candles from key %q: %w", key, err)
	}
	return gcs.Candles, nil
}

func MergeCandles(candles []*gobs.Candle, granularity time.Duration) ([]*gobs.Candle, error) {
	if granularity < candles[0].Duration {
		return nil, fmt.Errorf("desired granularity is not feasible")
	}
	if granularity%candles[0].Duration != 0 {
		return nil, fmt.Errorf("desired granularity must be a multiple of available candles' granularity")
	}

	var result []*gobs.Candle
	var tmpCandle *gobs.Candle
	var tmpEndTime time.Time
	for _, c := range candles {
		if tmpCandle != nil && !c.StartTime.Time.Before(tmpEndTime) {
			result = append(result, tmpCandle)
			tmpCandle = nil
		}

		if tmpCandle == nil {
			tmpCandle = &gobs.Candle{
				StartTime: c.StartTime,
				Duration:  granularity,
				Low:       c.Low,
				High:      c.High,
				Open:      c.Open,
				Close:     c.Close,
				Volume:    c.Volume,
			}
			tmpEndTime = tmpCandle.StartTime.Time.Add(tmpCandle.Duration)
		}

		tmpCandle.Low = decimal.Min(tmpCandle.Low, c.Low)
		tmpCandle.High = decimal.Max(tmpCandle.High, c.High)

		// tmpCandle.Open = tmpCandle.Open // Doesn't change
		tmpCandle.Close = c.Close

		tmpCandle.Volume = tmpCandle.Volume.Add(c.Volume)
	}
	return result, nil
}
