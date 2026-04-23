package main

import (
	"fmt"
	"os"

	"github.com/rpcarvs/reasond/cmd"
)

// version is injected at build time for tagged releases.
var version string

func main() {
	if err := cmd.Execute(version); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
