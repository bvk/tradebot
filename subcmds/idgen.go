// Copyright (c) 2023 BVK Chaitanya

package subcmds

import (
	"context"
	"flag"
	"fmt"

	"github.com/bvk/tradebot/idgen"
	"github.com/visvasity/cli"
)

type IDGen struct {
	from  uint64
	count int
}

func (c *IDGen) run(ctx context.Context, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("this command takes one (idgen seed) argument")
	}
	seed := args[0]
	gen := idgen.New(seed, c.from)
	for i := 0; i < c.count; i++ {
		offset, id := gen.Offset(), gen.NextID()
		fmt.Printf("%d: %s\n", offset, id)
	}
	return nil
}

func (c *IDGen) Command() (string, *flag.FlagSet, cli.CmdFunc) {
	fset := flag.NewFlagSet("idgen", flag.ContinueOnError)
	fset.Uint64Var(&c.from, "from", 0, "initial id offset")
	fset.IntVar(&c.count, "count", 10, "number of uuids")
	return "idgen", fset, cli.CmdFunc(c.run)
}

func (c *IDGen) Purpose() string {
	return "Prints client-order-ids for a seed string"
}
