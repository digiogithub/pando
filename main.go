package main

import (
	"github.com/digiogithub/pando/cmd"
	"github.com/digiogithub/pando/internal/logging"
)

func main() {
	defer logging.RecoverPanic("main", func() {
		logging.ErrorPersist("Application terminated due to unhandled panic")
	})

	cmd.Execute()
}
