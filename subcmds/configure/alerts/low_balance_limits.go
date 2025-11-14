// Copyright (c) 2025 BVK Chaitanya

package alerts

import (
	"context"
	"flag"
	"fmt"
	"regexp"
	"strings"

	"github.com/bvk/tradebot/gobs"
	"github.com/bvk/tradebot/kvutil"
	"github.com/bvk/tradebot/server"
	"github.com/bvk/tradebot/subcmds/cmdutil"
	"github.com/shopspring/decimal"
	"github.com/visvasity/cli"
)

type LowBalanceLimits struct {
	cmdutil.DBFlags
}

func (c *LowBalanceLimits) Purpose() string {
	return "Adds or updates lower-limits to raise alert on asset balance"
}

func (c *LowBalanceLimits) Command() (string, *flag.FlagSet, cli.CmdFunc) {
	fset := new(flag.FlagSet)
	c.DBFlags.SetFlags(fset)
	return "low-balance-limits", fset, cli.CmdFunc(c.run)
}

// Example: tradebot configure alerts low-balance-limits BCH=100 USD=200
func (c *LowBalanceLimits) run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("this command takes one or more arguments")
	}

	limitsMap := make(map[string]decimal.Decimal)
	for _, arg := range args {
		vs := strings.SplitN(arg, "=", 2)
		if len(vs) != 2 {
			return fmt.Errorf("invalid SYMBOL=LIMIT argument %q", arg)
		}

		// We expect all crypto symbol names be all-capitals with at least two
		// letters.
		if matched, err := regexp.MatchString("^[A-Z][A-Z]+$", vs[0]); err != nil {
			return err
		} else if !matched {
			return fmt.Errorf("unsupported/invalid symbol name in %q", arg)
		}

		limit, err := decimal.NewFromString(vs[1])
		if err != nil {
			return fmt.Errorf("invalid limit value in %q", arg)
		}
		limitsMap[vs[0]] = limit
	}

	db, closer, err := c.DBFlags.GetDatabase(ctx)
	if err != nil {
		return fmt.Errorf("could not create database client: %w", err)
	}
	defer closer()

	tx, err := db.NewTransaction(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	state, err := kvutil.Get[gobs.ServerState](ctx, tx, server.ServerStateKey)
	if err != nil {
		return err
	}
	if state.AlertsConfig == nil {
		state.AlertsConfig = new(gobs.AlertsConfig)
	}
	if state.AlertsConfig.LowBalanceLimits == nil {
		state.AlertsConfig.LowBalanceLimits = make(map[string]decimal.Decimal)
	}
	// Update or add SYMBOL=LIMIT entries.
	for k, v := range limitsMap {
		state.AlertsConfig.LowBalanceLimits[k] = v
	}

	if err := kvutil.Set[gobs.ServerState](ctx, tx, server.ServerStateKey, state); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	return nil
}
