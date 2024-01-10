// Copyright (c) 2023 BVK Chaitanya

package fix

import (
	"context"
	"flag"
	"fmt"
	"log"
	"maps"
	"os"
	"os/signal"
	"path"
	"strings"
	"syscall"
	"time"

	"github.com/bvk/tradebot/cli"
	"github.com/bvk/tradebot/coinbase"
	"github.com/bvk/tradebot/gobs"
	"github.com/bvk/tradebot/idgen"
	"github.com/bvk/tradebot/kvutil"
	"github.com/bvk/tradebot/limiter"
	"github.com/bvk/tradebot/server"
	"github.com/bvk/tradebot/subcmds/cmdutil"
	"github.com/bvkgo/kv"
)

type ResolveOrders struct {
	cmdutil.DBFlags

	product string

	clientIDsFile string

	dryRun bool
}

func (c *ResolveOrders) Command() (*flag.FlagSet, cli.CmdFunc) {
	fset := flag.NewFlagSet("resolve-orders", flag.ContinueOnError)
	c.DBFlags.SetFlags(fset)
	fset.StringVar(&c.product, "product", "", "name of the coinbase product")
	fset.StringVar(&c.clientIDsFile, "client-ids-file", "/tmp/client-ids.dat", "temp file to save client ids")
	fset.BoolVar(&c.dryRun, "dry-run", true, "when true only prints the information")
	return fset, cli.CmdFunc(c.Run)
}

func (c *ResolveOrders) Run(ctx context.Context, args []string) error {
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, closer, err := c.DBFlags.GetDatabase(ctx)
	if err != nil {
		return err
	}
	defer closer()

	datastore := coinbase.NewDatastore(db)

	// Scan all orders in the datastore.
	cid2sidMap := make(map[string]string)
	sid2orderMap := make(map[string]*gobs.Order)
	scanner := func(order *gobs.Order) error {
		sid2orderMap[order.ServerOrderID] = order
		if len(order.ClientOrderID) > 0 {
			cid2sidMap[order.ClientOrderID] = order.ServerOrderID
		}
		return nil
	}
	var zero time.Time
	if err := datastore.ScanFilled(ctx, c.product, zero, zero, scanner); err != nil {
		return err
	}

	// Collect cid seed information for all jobs.
	uidSeedMap := make(map[string]string)
	uidOffsetMap := make(map[string]uint64)
	collect := func(ctx context.Context, r kv.Reader, key string, value *gobs.LimiterState) error {
		uid := strings.TrimPrefix(key, limiter.DefaultKeyspace)
		uidSeedMap[uid] = value.V2.ClientIDSeed
		uidOffsetMap[uid] = value.V2.ClientIDOffset
		return nil
	}
	begin := path.Join(limiter.DefaultKeyspace, server.MinUUID)
	end := path.Join(limiter.DefaultKeyspace, server.MaxUUID)
	if err := kvutil.AscendDB(ctx, db, begin, end, collect); err != nil {
		return fmt.Errorf("could not collect limiter seeds: %w", err)
	}

	// Match the client ids to job uids.
	unmatchedSid2OrderMap := maps.Clone(sid2orderMap)
	uid2ordersMap := make(map[string][]*gobs.Order)
	for uid, offset := range uidOffsetMap {
		if offset > 10000 {
			log.Printf("scanning %d client order ids of job %s", offset+100, uid)
		}
		seed := uidSeedMap[uid]
		for g := idgen.New(seed, 0); g.Offset() < offset+100; {
			clientOrderID := g.NextID().String()
			if sid, ok := cid2sidMap[clientOrderID]; ok {
				vs := uid2ordersMap[uid]
				uid2ordersMap[uid] = append(vs, sid2orderMap[sid])
				delete(unmatchedSid2OrderMap, sid)
			}
		}
	}
	log.Printf("%d orders (out of %d) couldn't be resolved to job uid", len(unmatchedSid2OrderMap), len(cid2sidMap))

	// Verify that job data contains all it's orders.
	verify := func(ctx context.Context, r kv.Reader) error {
		for uid, orders := range uid2ordersMap {
			key := path.Join(limiter.DefaultKeyspace, uid)
			state, err := kvutil.Get[gobs.LimiterState](ctx, r, key)
			if err != nil {
				return fmt.Errorf("could not load limiter state at %q: %w", uid, err)
			}
			for _, order := range orders {
				if _, ok := state.V2.ServerIDOrderMap[order.ServerOrderID]; !ok {
					log.Printf("%s: has no order %v", uid, order)
				}
			}
		}
		return nil
	}
	if err := kv.WithReader(ctx, db, verify); err != nil {
		return fmt.Errorf("could not complete job state verification: %w", err)
	}

	if c.dryRun {
		return nil
	}

	update := func(ctx context.Context, rw kv.ReadWriter) error {
		for uid, orders := range uid2ordersMap {
			key := path.Join(limiter.DefaultKeyspace, uid)
			state, err := kvutil.Get[gobs.LimiterState](ctx, rw, key)
			if err != nil {
				return fmt.Errorf("could not load limiter state at %q: %w", key, err)
			}
			modified := false
			for _, order := range orders {
				sid := order.ServerOrderID
				if _, ok := state.V2.ServerIDOrderMap[sid]; ok {
					continue
				}
				modified = true
				state.V2.ServerIDOrderMap[sid] = order
			}
			if modified {
				if err := kvutil.Set(ctx, rw, key, state); err != nil {
					return fmt.Errorf("could not update limiter state at %q: %w", key, err)
				}
			}
		}
		return nil
	}
	if err := kv.WithReadWriter(ctx, db, update); err != nil {
		return fmt.Errorf("could not fix jobs state: %w", err)
	}
	return nil
}

func (c *ResolveOrders) Synopsis() string {
	return "Resolves filled exchange orders back to the job uids."
}
