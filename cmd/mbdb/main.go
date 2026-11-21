package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/dipeshc/mybrightday-backup/internal/app"
	"github.com/dipeshc/mybrightday-backup/internal/cmd"
)

// exitLoginRequired lets a wrapper (the GitHub workflow) tell "a login is
// needed, links were printed" apart from any other failure.
const exitLoginRequired = 3

func main() {
	if err := cmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		if errors.Is(err, app.ErrLoginRequired) {
			os.Exit(exitLoginRequired)
		}
		os.Exit(1)
	}
}
