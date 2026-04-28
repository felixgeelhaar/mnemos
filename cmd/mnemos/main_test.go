package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFindProjectDB_NoMnemosDirReturnsFalse(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Chdir(root)

	got, _, ok := findProjectDB()
	if ok {
		t.Fatalf("expected no project DB, got %q", got)
	}
}

func TestFindProjectDB_FindsInCurrentDirectory(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	if err := os.Mkdir(filepath.Join(root, ".mnemos"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	t.Chdir(root)

	got, _, ok := findProjectDB()
	if !ok {
		t.Fatal("expected project DB, got none")
	}
	want := filepath.Join(root, ".mnemos", "mnemos.db")
	if got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
}

func TestFindProjectDB_WalksUpToAncestor(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	if err := os.Mkdir(filepath.Join(root, ".mnemos"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	deep := filepath.Join(root, "a", "b", "c")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatalf("mkdirall: %v", err)
	}
	t.Chdir(deep)

	got, _, ok := findProjectDB()
	if !ok {
		t.Fatal("expected project DB, got none")
	}
	want := filepath.Join(root, ".mnemos", "mnemos.db")
	if got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
}

func TestFindProjectDB_PrefersNearestAncestor(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	if err := os.Mkdir(filepath.Join(root, ".mnemos"), 0o755); err != nil {
		t.Fatalf("mkdir outer: %v", err)
	}
	inner := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(filepath.Join(inner, ".mnemos"), 0o755); err != nil {
		t.Fatalf("mkdir inner: %v", err)
	}
	deep := filepath.Join(inner, "c")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatalf("mkdirall: %v", err)
	}
	t.Chdir(deep)

	got, _, ok := findProjectDB()
	if !ok {
		t.Fatal("expected project DB, got none")
	}
	want := filepath.Join(inner, ".mnemos", "mnemos.db")
	if got != want {
		t.Fatalf("path = %q, want %q (should prefer nearest ancestor)", got, want)
	}
}

func TestFindProjectDB_StopsAtHomeDirectory(t *testing.T) {
	root := t.TempDir()
	// .mnemos lives ABOVE the configured HOME, so discovery must stop before it.
	if err := os.Mkdir(filepath.Join(root, ".mnemos"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatalf("mkdir home: %v", err)
	}
	t.Setenv("HOME", home)
	t.Chdir(home)

	if got, _, ok := findProjectDB(); ok {
		t.Fatalf("expected discovery to stop at HOME, got %q", got)
	}
}

func TestResolveDBPath_ProjectBeatsGlobalDefault(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "xdg"))
	if err := os.Mkdir(filepath.Join(root, ".mnemos"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	t.Chdir(root)

	want := filepath.Join(root, ".mnemos", "mnemos.db")
	if got := resolveDBPath(); got != want {
		t.Fatalf("resolveDBPath = %q, want %q (project should win over XDG)", got, want)
	}
}

func TestResolveDBPath_FallsBackToXDGGlobal(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	xdg := filepath.Join(root, "xdg")
	t.Setenv("XDG_DATA_HOME", xdg)
	t.Chdir(root)

	want := filepath.Join(xdg, "mnemos", "mnemos.db")
	if got := resolveDBPath(); got != want {
		t.Fatalf("resolveDBPath = %q, want %q", got, want)
	}
}
