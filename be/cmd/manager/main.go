package main

import (
	"fmt"
	"os"

	"github.com/anish-araz_cumulus/firecracker-manager-go/internal/cli"
)

func main() {
	if err := cli.NewRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
