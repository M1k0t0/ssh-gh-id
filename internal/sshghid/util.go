package sshghid

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type multiError struct {
	parts []string
}

func boolPtr(v bool) *bool { return &v }

func (m *multiError) add(format string, args ...any) {
	m.parts = append(m.parts, fmt.Sprintf(format, args...))
}

func (m *multiError) err() error {
	if len(m.parts) == 0 {
		return nil
	}
	return errors.New(strings.Join(m.parts, "; "))
}

func addUnique(users *[]string, username string) bool {
	for _, u := range *users {
		if u == username {
			return false
		}
	}
	*users = append(*users, username)
	sort.Strings(*users)
	return true
}

func removeValue(users []string, username string) ([]string, bool) {
	out := make([]string, 0, len(users))
	found := false
	for _, u := range users {
		if u == username {
			found = true
			continue
		}
		out = append(out, u)
	}
	return out, found
}

func contains(users []string, username string) bool {
	for _, u := range users {
		if u == username {
			return true
		}
	}
	return false
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func expandHome(path, home string) string {
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(home, strings.TrimPrefix(path, "~/"))
	}
	return path
}

func writeFileAtomic(path string, content []byte, mode os.FileMode) error {
	return writeFileAtomicWithExistingMode(path, content, mode)
}

func writeFileAtomicWithExistingMode(path string, content []byte, fallbackMode os.FileMode) error {
	mode := fallbackMode
	if st, err := os.Stat(path); err == nil {
		mode = st.Mode().Perm()
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp.")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(content); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	return syncDir(filepath.Dir(path))
}

func writeReaderAtomic(path string, r io.Reader, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp.")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if _, err := io.Copy(tmp, r); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	return syncDir(filepath.Dir(path))
}

func syncDir(path string) error {
	d, err := os.Open(path)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

func readNonEmptyLines(r io.Reader) ([]string, error) {
	s := bufio.NewScanner(r)
	var lines []string
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "" {
			continue
		}
		lines = append(lines, line)
	}
	return lines, s.Err()
}

func shellEscape(s string) string {
	if s == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'"
}
