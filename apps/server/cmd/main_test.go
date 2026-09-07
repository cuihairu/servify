package main

import (
	"os"
	"testing"
)

func TestMainInvokesCLI(t *testing.T) {
	origArgs := os.Args
	os.Args = []string{"servify", "version"}
	defer func() { os.Args = origArgs }()

	// main delegates to cli.Execute which runs the version command and
	// returns without exiting.
	main()
}
