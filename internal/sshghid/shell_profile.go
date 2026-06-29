package sshghid

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func pathContains(binPath string) bool {
	binDir := filepath.Dir(binPath)
	for _, part := range filepath.SplitList(os.Getenv("PATH")) {
		if part == binDir {
			return true
		}
	}
	return false
}

func (a *App) ensureLocalBinOnPath() (string, bool, error) {
	profilePath := a.preferredShellProfile()
	if err := os.MkdirAll(filepath.Dir(profilePath), 0o755); err != nil {
		return profilePath, false, err
	}
	current, err := os.ReadFile(profilePath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return profilePath, false, err
	}
	binDir := filepath.Dir(a.LocalBinPath)
	exportLine := fmt.Sprintf("export PATH=%s:$PATH", shellEscape(binDir))
	block := strings.Join([]string{
		pathStartMarker,
		exportLine,
		pathEndMarker,
	}, "\n") + "\n"
	updated, changed, err := replaceManagedPathBlock(string(current), block)
	if err != nil {
		return profilePath, false, err
	}
	if !changed {
		return profilePath, false, nil
	}
	if err := writeFileAtomic(profilePath, []byte(updated), 0o644); err != nil {
		return profilePath, false, err
	}
	return profilePath, true, nil
}

func (a *App) preferredShellProfile() string {
	shell := strings.ToLower(filepath.Base(os.Getenv("SHELL")))
	switch shell {
	case "zsh":
		return filepath.Join(a.Home, ".zshrc")
	case "bash":
		return filepath.Join(a.Home, ".bashrc")
	default:
		return filepath.Join(a.Home, ".profile")
	}
}

func replaceManagedPathBlock(current, block string) (string, bool, error) {
	start := strings.Index(current, pathStartMarker)
	end := strings.Index(current, pathEndMarker)
	switch {
	case start == -1 && end == -1:
		if strings.TrimSpace(current) == "" {
			return block, true, nil
		}
		trimmed := strings.TrimRight(current, "\n") + "\n\n" + block
		return trimmed, true, nil
	case start == -1 || end == -1 || end < start:
		return "", false, fmt.Errorf("invalid managed PATH block in shell profile")
	default:
		end += len(pathEndMarker)
		if end < len(current) && current[end] == '\n' {
			end++
		}
		next := current[:start] + block + current[end:]
		if next == current {
			return current, false, nil
		}
		return next, true, nil
	}
}
