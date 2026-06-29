package main

import (
	"fmt"
	"os"

	"ssh-gh-id/internal/sshghid"
)

func main() {
	if err := sshghid.Run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
