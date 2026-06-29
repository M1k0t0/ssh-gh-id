package sshghid

import (
	"os"
	"strings"
)

const (
	ansiReset  = "[0m"
	ansiBold   = "[1m"
	ansiDim    = "[2m"
	ansiRed    = "[31m"
	ansiGreen  = "[32m"
	ansiYellow = "[33m"
	ansiBlue   = "[34m"
	ansiCyan   = "[36m"
)

func useColor() bool {
	term := strings.ToLower(os.Getenv("TERM"))
	return term != "" && term != "dumb" && os.Getenv("NO_COLOR") == ""
}

func colorize(code, s string) string {
	if !useColor() {
		return s
	}
	return code + s + ansiReset
}

func successText(s string) string { return colorize(ansiGreen, s) }

func warnText(s string) string { return colorize(ansiYellow, s) }

func infoText(s string) string { return colorize(ansiCyan, s) }

func errorText(s string) string { return colorize(ansiRed, s) }

func keyText(s string) string { return colorize(ansiBlue, s) }

func titleText(s string) string { return colorize(ansiBold, s) }

func dimText(s string) string { return colorize(ansiDim, s) }
