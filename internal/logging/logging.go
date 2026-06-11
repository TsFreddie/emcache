package logging

import (
	"fmt"
	"os"
)

var verbose = os.Getenv("VERBOSE") == "1"

func Printf(format string, args ...any) {
	fmt.Printf(format, args...)
}

func Verbosef(format string, args ...any) {
	if verbose {
		fmt.Printf(format, args...)
	}
}

func Verbose() bool {
	return verbose
}
