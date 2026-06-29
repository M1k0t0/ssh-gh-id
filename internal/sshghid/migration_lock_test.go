package sshghid

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestHandleRunMigrationsRequiresInheritedLockFD(t *testing.T) {
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
	if err == nil || !strings.Contains(err.Error(), "requires inherited lock fd") {
		t.Fatalf("expected lock error, got %v", err)
	}

	t.Setenv("SSH_GH_ID_INTERNAL_MIGRATION", "1")
	if err := app.handleRunMigrations("0.3.1", "0.3.2"); err == nil || !strings.Contains(err.Error(), "requires inherited lock fd") {
		t.Fatalf("expected lock error with internal env only, got %v", err)
	}
}

func TestHandleRunMigrationsUsesInheritedLockFD(t *testing.T) {
	tmp := t.TempDir()
	app := &App{
		StateDir: filepath.Join(tmp, "state"),
		LockPath: filepath.Join(tmp, "state", "lock"),
	}
	lock, err := app.acquireLockHandle()
	if err != nil {
		t.Fatal(err)
	}
	defer lock.release()

	dupFD, err := syscall.Dup(int(lock.file.Fd()))
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("SSH_GH_ID_LOCK_FD", fmt.Sprint(dupFD))
	if err := app.handleRunMigrations("0.3.1", "0.3.2"); err != nil {
		t.Fatal(err)
	}

	secondUnlock, err := app.acquireLock()
	if err == nil {
		secondUnlock()
		t.Fatal("parent lock was released by inherited fd")
	}
	if !strings.Contains(err.Error(), "another ssh-gh-id process is already running") {
		t.Fatalf("expected parent lock to remain held, got %v", err)
	}
}

func TestHandleRunMigrationsRejectsWrongInheritedLockFD(t *testing.T) {
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

	wrong, err := os.OpenFile(filepath.Join(tmp, "wrong-lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer wrong.Close()

	t.Setenv("SSH_GH_ID_LOCK_FD", fmt.Sprint(wrong.Fd()))
	err = app.handleRunMigrations("0.3.1", "0.3.2")
	if err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("expected lock fd mismatch, got %v", err)
	}
}

func TestHandleRunMigrationsRejectsUnlockedLockFD(t *testing.T) {
	tmp := t.TempDir()
	app := &App{
		StateDir: filepath.Join(tmp, "state"),
		LockPath: filepath.Join(tmp, "state", "lock"),
	}
	if err := os.MkdirAll(app.StateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	unlocked, err := os.OpenFile(app.LockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer unlocked.Close()

	t.Setenv("SSH_GH_ID_LOCK_FD", fmt.Sprint(unlocked.Fd()))
	err = app.handleRunMigrations("0.3.1", "0.3.2")
	if err == nil || !strings.Contains(err.Error(), "not held by a parent self-update process") {
		t.Fatalf("expected unlocked fd rejection, got %v", err)
	}
}
