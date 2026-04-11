// Command gcode is the CLI entry point. It dispatches between the
// print (pipe) mode and the interactive TUI mode.
package main

import (
	"fmt"
	"os"
)

func main() {
	args, ok := ParseArgs(os.Args[1:])
	if !ok {
		os.Exit(2)
	}

	// Default to print mode when stdin is not a terminal (shell pipe).
	if !args.PrintMode && !args.JSONMode && isStdinPipe() {
		args.PrintMode = true
	}

	if args.PrintMode {
		os.Exit(runPrintMode(args))
	}
	if args.JSONMode {
		fmt.Fprintln(os.Stderr, "gcode: JSON mode is not implemented yet")
		os.Exit(2)
	}
	os.Exit(runInteractiveMode(args))
}

// isStdinPipe reports whether stdin is a pipe or file (i.e. not a tty).
func isStdinPipe() bool {
	info, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return (info.Mode() & os.ModeCharDevice) == 0
}
