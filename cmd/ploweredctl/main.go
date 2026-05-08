// Command ploweredctl is the operator CLI: migrations, debug, admin.
package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: ploweredctl <command>")
		fmt.Fprintln(os.Stderr, "commands: migrate, version, asset, connector")
		os.Exit(2)
	}
	// TODO(M1): implement subcommands using cobra or stdlib flag.
	switch os.Args[1] {
	case "version":
		fmt.Println("ploweredctl dev")
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		os.Exit(2)
	}
}
