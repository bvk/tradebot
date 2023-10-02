// Copyright (c) 2023 BVK Chaitanya

package main

import (
	"context"
	"flag"
	"os"

	"github.com/bvkgo/tradebot/coinbase"
	"github.com/bvkgo/tradebot/exchange"
	"github.com/google/subcommands"
)

var _ exchange.Product = &coinbase.Product{}

func main() {
	subcommands.Register(subcommands.HelpCommand(), "")
	subcommands.Register(subcommands.FlagsCommand(), "")
	subcommands.Register(subcommands.CommandsCommand(), "")
	subcommands.Register(&runCmd{}, "")

	flag.Parse()
	ctx := context.Background()
	os.Exit(int(subcommands.Execute(ctx)))
}
