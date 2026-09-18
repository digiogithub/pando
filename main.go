package main

import (
	"github.com/digiogithub/pando/cmd"
	// The sandbox re-exec helper (`pando __sandbox-exec ...`, Linux) dispatches
	// from this package's init, before cobra, config and logging start.
	_ "github.com/digiogithub/pando/internal/sandbox/helper"
)

func main() {
	cmd.Main()
}
