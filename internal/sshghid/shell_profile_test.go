package sshghid

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReplaceManagedPathBlockAppendsReplacesAndNoops(t *testing.T) {
	block := pathStartMarker + "\nexport PATH='/one':$PATH\n" + pathEndMarker + "\n"
	out, changed, err := replaceManagedPathBlock("echo hi\n", block)
	if err != nil {
		t.Fatal(err)
	}
	if !changed || !strings.Contains(out, "echo hi") || !strings.Contains(out, "'/one'") {
		t.Fatalf("append failed changed=%v out=%q", changed, out)
	}
	out, changed, err = replaceManagedPathBlock(out, block)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatalf("same block should be no-op, got changed out=%q", out)
	}
	replacement := pathStartMarker + "\nexport PATH='/two':$PATH\n" + pathEndMarker + "\n"
	out, changed, err = replaceManagedPathBlock(out, replacement)
	if err != nil {
		t.Fatal(err)
	}
	if !changed || strings.Contains(out, "'/one'") || !strings.Contains(out, "'/two'") {
		t.Fatalf("replace failed changed=%v out=%q", changed, out)
	}
}

func TestReplaceManagedPathBlockRejectsIncompleteBlock(t *testing.T) {
	_, _, err := replaceManagedPathBlock(pathStartMarker+"\nexport PATH=/bad:$PATH\n", "")
	if err == nil || !strings.Contains(err.Error(), "invalid") {
		t.Fatalf("expected invalid path block error, got %v", err)
	}
}

func TestPreferredShellProfile(t *testing.T) {
	app := &App{Home: "/home/alice"}
	cases := map[string]string{
		"/bin/zsh":      "/home/alice/.zshrc",
		"/usr/bin/bash": "/home/alice/.bashrc",
		"/bin/fish":     "/home/alice/.profile",
	}
	for shell, want := range cases {
		t.Setenv("SHELL", shell)
		if got := app.preferredShellProfile(); got != want {
			t.Fatalf("preferredShellProfile(%q)=%q want %q", shell, got, want)
		}
	}
}

func TestPathContains(t *testing.T) {
	t.Setenv("PATH", strings.Join([]string{"/usr/bin", "/home/alice/bin"}, string(os.PathListSeparator)))
	if !pathContains("/home/alice/bin/ssh-gh-id") {
		t.Fatal("expected PATH to contain binary directory")
	}
	if pathContains("/opt/ssh-gh-id") {
		t.Fatal("did not expect PATH to contain /opt")
	}
}

func TestEnsureLocalBinOnPathWritesManagedBlockOnce(t *testing.T) {
	tmp := t.TempDir()
	app := &App{Home: tmp, LocalBinPath: filepath.Join(tmp, "bin", appName)}
	t.Setenv("SHELL", "/bin/zsh")
	profile, changed, err := app.ensureLocalBinOnPath()
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected first ensureLocalBinOnPath call to change profile")
	}
	if profile != filepath.Join(tmp, ".zshrc") {
		t.Fatalf("profile=%q", profile)
	}
	content, err := os.ReadFile(profile)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(content), pathStartMarker) != 1 || !strings.Contains(string(content), filepath.Join(tmp, "bin")) {
		t.Fatalf("unexpected profile content:\n%s", content)
	}
	_, changed, err = app.ensureLocalBinOnPath()
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("second ensureLocalBinOnPath call should be no-op")
	}
}
