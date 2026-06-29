package sshghid

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestHandleRunMigrationsSkipsLockWhenParentHoldsIt(t *testing.T) {
	tmp := t.TempDir()
	app := &App{
		StateDir: filepath.Join(tmp, "state"),
		LockPath: filepath.Join(tmp, "state", "lock"),
	}
	unlock, err := app.acquireLock()
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()

	err = app.handleRunMigrations("0.3.1", "0.3.2")
	if err == nil || !strings.Contains(err.Error(), "another ssh-gh-id process is already running") {
		t.Fatalf("expected lock error, got %v", err)
	}

	t.Setenv("SSH_GH_ID_PARENT_LOCK_HELD", "1")
	if err := app.handleRunMigrations("0.3.1", "0.3.2"); err != nil {
		t.Fatal(err)
	}
}
