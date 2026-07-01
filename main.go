package main

import (
	"fmt"
	"os"

	"github.com/M1k0t0/ssh-gh-id/internal/sshghid"
)

func main() {
	if err := sshghid.Run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
