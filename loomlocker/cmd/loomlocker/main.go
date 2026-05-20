package main

import (
	"fmt"
	"os"

	"github.com/sayandeepgiri/promptloom/loomlocker/cli"
)

func main() {
	if err := cli.RootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}
