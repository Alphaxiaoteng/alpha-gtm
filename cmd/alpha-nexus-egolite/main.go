package main

import (
	"fmt"
	"os"

	"alpha-nexus-egolite/internal/cli"
)

func main() {
	root := cli.NewRootCmd()
	// Cobra Print/Printf/Println use OutOrStderr (stderr) unless SetOut is set.
	// Prefer stdout for normal command output so scripts can capture $(cmd).
	root.SetOut(os.Stdout)
	root.SetErr(os.Stderr)
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
