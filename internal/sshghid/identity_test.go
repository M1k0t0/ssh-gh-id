package sshghid

import (
	"testing"
)

func TestNormalizeUsername(t *testing.T) {
	got, err := normalizeUsername(" Foo-Bar ")
	if err != nil {
		t.Fatal(err)
	}
	if got != "foo-bar" {
		t.Fatalf("got %q", got)
	}
	if _, err := normalizeUsername("bad_name"); err == nil {
		t.Fatal("expected invalid username error")
	}
}
