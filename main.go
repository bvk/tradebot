// Copyright (c) 2023 BVK Chaitanya

package main

import (
	"context"
	"log"
	"os"

	"github.com/bvkgo/tradebot/cli"
	"github.com/bvkgo/tradebot/coinbase"
	"github.com/bvkgo/tradebot/exchange"
	"github.com/bvkgo/tradebot/subcmds"
	"github.com/bvkgo/tradebot/subcmds/db"
)

var _ exchange.Product = &coinbase.Product{}

func main() {
	dbcmds := []cli.Command{
		new(db.Get),
		new(db.Set),
		new(db.Delete),
		new(db.List),
	}

	cmds := []cli.Command{
		new(subcmds.Run),
		cli.CommandGroup("db", dbcmds...),
	}
	if err := cli.Run(context.Background(), cmds, os.Args[1:]); err != nil {
		log.Fatal(err)
	}
}
