// Copyright (c) 2025 BVK Chaitanya

package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/bvk/tradebot/exchange"
	"github.com/bvk/tradebot/gobs"
	"github.com/bvk/tradebot/kvutil"
	"github.com/shopspring/decimal"
	"github.com/visvasity/topic"
)

func (s *Server) watchForLowBalance(ctx context.Context, ex exchange.Exchange) error {
	updates, err := ex.GetBalanceUpdates()
	if err != nil {
		return err
	}
	defer updates.Close()

	updatesCh, err := topic.ReceiveCh(updates)
	if err != nil {
		return err
	}

	exname := ex.ExchangeName()

	for {
		select {
		case <-ctx.Done():
			return context.Cause(ctx)

		case update := <-updatesCh:
			ccy, amount := update.Balance()
			if err := s.alertOnLowBalance(ctx, exname, ccy, amount); err != nil {
				slog.Warn("could not send low balance alert", "exchange", exname, "currency", ccy, "amount", amount)
			}
		}
	}
}

func (s *Server) alertOnLowBalance(ctx context.Context, exchangeName, currency string, amount decimal.Decimal) error {
	now := time.Now()
	ccy := strings.ToUpper(currency)
	exchange := strings.ToLower(exchangeName)
	key := fmt.Sprintf("alerts/low-balance-alert/%s/%s", exchange, ccy)
	if deadline, ok := s.alertFreezeDeadlineMap[key]; ok {
		if now.Before(deadline) {
			return nil
		}
		delete(s.alertFreezeDeadlineMap, key)
	}

	state, err := kvutil.GetDB[gobs.ServerState](ctx, s.db, ServerStateKey)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	// No alerts config is not an error.
	if state.AlertsConfig == nil {
		return nil
	}
	// Per exchange alerts config takes higher precedence if it exists.
	if cfg, ok := state.AlertsConfig.PerExchangeConfig[exchange]; ok && cfg != nil && cfg.LowBalanceLimits != nil {
		if limit, ok := cfg.LowBalanceLimits[strings.ToUpper(ccy)]; ok {
			if amount.LessThanOrEqual(limit) {
				s.SendMessage(ctx, now,
					"Available balance %s for %q in exchange %s is below the exchange specific limit %s.",
					amount.StringFixed(5), ccy, exchange, limit)
				s.alertFreezeDeadlineMap[key] = now.Add(time.Hour)
				return nil
			}
			return nil
		}
		// This asset doesn't have per-exchange limits, so fallback to check the default limits.
	}

	if limit, ok := state.AlertsConfig.LowBalanceLimits[strings.ToUpper(ccy)]; ok {
		if amount.LessThanOrEqual(limit) {
			s.SendMessage(ctx, time.Now(),
				"Available balance %s for %q in exchange %s is below the default limit %s.",
				amount.StringFixed(5), ccy, exchange, limit)
			s.alertFreezeDeadlineMap[key] = now.Add(time.Hour)
			return nil
		}
	}
	return nil
}
