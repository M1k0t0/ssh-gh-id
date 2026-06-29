package sshghid

import (
	"testing"
)

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		left  string
		right string
		want  int
	}{
		{left: "v0.3.2", right: "0.3.1", want: 1},
		{left: "0.10.0", right: "0.9.9", want: 1},
		{left: "1.2", right: "v1.2.0", want: 0},
		{left: "1.2.3+build", right: "1.2.3", want: 0},
		{left: "1.2.3", right: "1.2.4", want: -1},
	}
	for _, tc := range cases {
		got, err := compareVersions(tc.left, tc.right)
		if err != nil {
			t.Fatalf("compareVersions(%q, %q): %v", tc.left, tc.right, err)
		}
		if got != tc.want {
			t.Fatalf("compareVersions(%q, %q)=%d want %d", tc.left, tc.right, got, tc.want)
		}
	}
}

func TestMigrationApplies(t *testing.T) {
	applies, err := migrationApplies("0.3.1", "0.4.0", "0.3.2")
	if err != nil {
		t.Fatal(err)
	}
	if !applies {
		t.Fatal("expected migration to apply")
	}
	applies, err = migrationApplies("0.3.2", "0.4.0", "0.3.2")
	if err != nil {
		t.Fatal(err)
	}
	if applies {
		t.Fatal("migration should not rerun when source is already at migration version")
	}
}
