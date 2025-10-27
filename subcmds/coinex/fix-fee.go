// Copyright (c) 2025 BVK Chaitanya

package coinex

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"path"
	"strconv"
	"strings"
	"syscall"

	"github.com/bvk/tradebot/coinex"
	"github.com/bvk/tradebot/gobs"
	"github.com/bvk/tradebot/kvutil"
	"github.com/bvk/tradebot/limiter"
	"github.com/bvk/tradebot/server"
	"github.com/bvk/tradebot/subcmds/cmdutil"
	"github.com/bvkgo/kv"
	"github.com/visvasity/cli"
)

type FixFee struct {
	cmdutil.DBFlags

	secretsPath string

	dryRun bool
}

func (c *FixFee) Purpose() string {
	return "Fix for incorrect fee data for one or more limiter jobs."
}

func (c *FixFee) Command() (string, *flag.FlagSet, cli.CmdFunc) {
	fset := flag.NewFlagSet("fix-fee", flag.ContinueOnError)
	c.DBFlags.SetFlags(fset)
	fset.StringVar(&c.secretsPath, "secrets-file", "", "path to credentials file")
	fset.BoolVar(&c.dryRun, "dry-run", false, "when true only counts fee inconsistencies")
	return "fix-fee", fset, cli.CmdFunc(c.run)
}

func (c *FixFee) run(ctx context.Context, args []string) error {
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	if len(c.secretsPath) == 0 {
		return fmt.Errorf("coinex secrets file path is required")
	}
	secrets, err := server.SecretsFromFile(c.secretsPath)
	if err != nil {
		return err
	}
	if secrets.CoinEx == nil {
		return fmt.Errorf("secrets file has no coinex credentials")
	}

	db, closer, err := c.DBFlags.GetDatabase(ctx)
	if err != nil {
		return err
	}
	defer closer()

	keys := make([]string, len(args))
	for i, arg := range args {
		if strings.HasPrefix(arg, limiter.DefaultKeyspace) {
			keys[i] = arg
			continue
		}
		keys[i] = path.Join(limiter.DefaultKeyspace, arg)
	}

	// Read all limiter uids when len(args) == 0
	if len(keys) == 0 {
		collector := func(ctx context.Context, r kv.Reader) error {
			it, err := r.Ascend(ctx, limiter.DefaultKeyspace, "")
			if err != nil {
				return err
			}
			defer kv.Close(it)

			for k, _, err := it.Fetch(ctx, false); err == nil; k, _, err = it.Fetch(ctx, true) {
				if !strings.HasPrefix(k, limiter.DefaultKeyspace) {
					break
				}
				keys = append(keys, k)
			}
			if _, _, err := it.Fetch(ctx, false); err != nil && !errors.Is(err, io.EOF) {
				return err
			}
			return nil
		}
		if err := kv.WithReader(ctx, db, collector); err != nil {
			return err
		}
	}

	if len(keys) == 0 {
		return fmt.Errorf("no limiter keys found")
	}

	keyStateMap := make(map[string]*gobs.LimiterState)
	for _, key := range keys {
		state, err := kvutil.GetDB[gobs.LimiterState](ctx, db, key)
		if err != nil {
			return err
		}
		keyStateMap[key] = state
	}

	marketMap := make(map[string][]string)
	oidStateMap := make(map[string]*gobs.LimiterState)
	for key, state := range keyStateMap {
		if state.V2 == nil {
			return fmt.Errorf("v1 limiter %q is not supported: %w", key, os.ErrInvalid)
		}
		if !strings.EqualFold(state.V2.ExchangeName, "coinex") {
			return fmt.Errorf("limiter %q does not use coinex exchange: %w", key, os.ErrInvalid)
		}
		for oid, order := range state.V2.ServerIDOrderMap {
			if order.FilledSize.IsZero() {
				continue
			}
			if !order.Done {
				continue
			}
			if order.FilledFee.IsZero() {
				oids := marketMap[state.V2.ProductID]
				marketMap[state.V2.ProductID] = append(oids, oid)
				oidStateMap[oid] = state
			}
		}
	}

	log.Printf("found %d orders across %d products with zero fees", len(oidStateMap), len(marketMap))
	if c.dryRun || len(oidStateMap) == 0 {
		return nil
	}

	opts := &coinex.Options{
		NoWebsocket: true,
	}
	cex, err := coinex.New(ctx, secrets.CoinEx.Key, secrets.CoinEx.Secret, opts)
	if err != nil {
		return fmt.Errorf("could not create coinex client: %w", err)
	}
	defer cex.Close()

	for market, oids := range marketMap {
		ids := make([]int64, len(oids))
		for i, oid := range oids {
			id, err := strconv.ParseInt(oid, 10, 64)
			if err != nil {
				return err
			}
			ids[i] = id
		}

		for i := 0; i < len(oids); i += 25 {
			vs := ids[i:min(len(oids), i+25)]
			items, err := cex.BatchQueryOrders(ctx, market, vs)
			if err != nil {
				return err
			}

			for j, item := range items {
				oid := strconv.FormatInt(vs[j], 10)
				if item.Code != 0 {
					log.Printf("%s: %s (%d)", oid, item.Message, item.Code)
					continue
				}
				state := oidStateMap[oid]
				order := state.V2.ServerIDOrderMap[oid]
				order.FilledFee = item.Data.ExecutedFee()
			}
		}
	}

	for key, state := range keyStateMap {
		if err := kvutil.SetDB(ctx, db, key, state); err != nil {
			return err
		}
	}
	return nil
}
