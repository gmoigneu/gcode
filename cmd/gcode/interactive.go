package main

import (
	"fmt"
	"os"
)

// runInteractiveMode launches the full TUI. The implementation is added
// in a follow-up PR (#52); this stub prints a placeholder message so
// the print mode can ship independently.
func runInteractiveMode(args Args) int {
	fmt.Fprintln(os.Stderr, "gcode: interactive mode is not implemented yet; use --print")
	return 2
}
