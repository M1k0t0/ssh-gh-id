package sshghid

import "testing"

func TestInstallPathForRootUsesFixedPath(t *testing.T) {
	got := installPathFor("/home/alice", "/home/alice/Downloads/ssh-gh-id", true)
	if got != rootInstallPath {
		t.Fatalf("installPathFor(root)=%q want %q", got, rootInstallPath)
	}
}

func TestInstallPathForNonRootUsesExecutableDirectory(t *testing.T) {
	got := installPathFor("/home/alice", "/opt/tools/ssh-gh-id", false)
	want := "/opt/tools/ssh-gh-id"
	if got != want {
		t.Fatalf("installPathFor(non-root)=%q want %q", got, want)
	}
}

func TestInstallPathForNonRootFallsBackToLocalBin(t *testing.T) {
	got := installPathFor("/home/alice", "", false)
	want := "/home/alice/.local/bin/ssh-gh-id"
	if got != want {
		t.Fatalf("installPathFor(fallback)=%q want %q", got, want)
	}
}

func TestNewAppIgnoresKeysBaseURLEnvironment(t *testing.T) {
	t.Setenv("SSH_GH_ID_KEYS_BASE_URL", "https://attacker.example")
	app, err := newApp()
	if err != nil {
		t.Fatal(err)
	}
	if app.BaseURL != githubKeysBaseURL {
		t.Fatalf("BaseURL=%q want %q", app.BaseURL, githubKeysBaseURL)
	}
}
