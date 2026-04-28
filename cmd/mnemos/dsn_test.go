package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// TestResolveDSN_URLEnvWinsEverything covers the headline contract:
// when MNEMOS_DB_URL is set we return it untouched. The user might
// be pointing at memory://, postgres://, or any future scheme — we
// should not interpret the value as a file path.
func TestResolveDSN_URLEnvWinsEverything(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	if err := os.Mkdir(filepath.Join(root, ".mnemos"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	t.Chdir(root)

	t.Setenv("MNEMOS_DB_URL", "memory://")
	if got, want := resolveDSN(), "memory://"; got != want {
		t.Errorf("resolveDSN = %q, want %q (URL env should win even when project DB exists)", got, want)
	}

	t.Setenv("MNEMOS_DB_URL", "sqlite:///srv/cogstack.db?namespace=mnemos")
	if got := resolveDSN(); got != "sqlite:///srv/cogstack.db?namespace=mnemos" {
		t.Errorf("resolveDSN dropped query parameters: got %q", got)
	}
}

// TestResolveDSN_FallsBackToSQLiteFromProjectPath verifies the
// no-MNEMOS_DB_URL path: we wrap the resolved SQLite project file as
// sqlite://<path> so the registry can dispatch.
func TestResolveDSN_FallsBackToSQLiteFromProjectPath(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("MNEMOS_DB_URL", "")
	if err := os.Mkdir(filepath.Join(root, ".mnemos"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	t.Chdir(root)

	want := "sqlite://" + filepath.Join(root, ".mnemos", "mnemos.db")
	if got := resolveDSN(); got != want {
		t.Errorf("resolveDSN = %q, want %q", got, want)
	}
}

// TestResolveDSN_FallsBackToXDGGlobal covers the case where neither
// MNEMOS_DB_URL nor a .mnemos/ project directory exists: the resolver
// should still produce a usable sqlite:// DSN pointing at the XDG
// global default.
func TestResolveDSN_FallsBackToXDGGlobal(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("MNEMOS_DB_URL", "")
	xdg := filepath.Join(root, "xdg")
	t.Setenv("XDG_DATA_HOME", xdg)
	t.Chdir(root)

	want := "sqlite://" + filepath.Join(xdg, "mnemos", "mnemos.db")
	if got := resolveDSN(); got != want {
		t.Errorf("resolveDSN = %q, want %q", got, want)
	}
}

// TestOpenConn_OpensMemoryBackend exercises the helper end-to-end
// with the memory provider — proves the providers are registered in
// cmd/mnemos and that resolveDSN+store.Open compose correctly.
func TestOpenConn_OpensMemoryBackend(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("MNEMOS_DB_URL", "memory://")
	t.Chdir(root)

	conn, err := openConn(context.Background())
	if err != nil {
		t.Fatalf("openConn(memory): %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	if conn.Events == nil || conn.Claims == nil {
		t.Errorf("memory Conn missing repos: %+v", conn)
	}
}

// TestOpenConn_OpensSQLiteBackendFromURL mirrors the memory test for
// the SQLite provider — the registry should dispatch sqlite:// DSNs
// to the SQLite provider and surface a *sql.DB on Conn.Raw.
func TestOpenConn_OpensSQLiteBackendFromURL(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("MNEMOS_DB_URL", "sqlite://"+filepath.Join(root, "explicit.db"))
	t.Chdir(root)

	conn, err := openConn(context.Background())
	if err != nil {
		t.Fatalf("openConn(sqlite): %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	if conn.Raw == nil {
		t.Error("expected SQLite Conn to expose *sql.DB through Raw")
	}
}
