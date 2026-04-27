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
	t.Setenv("MNEMOS_DB_PATH", filepath.Join(root, "ignored.db"))

	t.Setenv("MNEMOS_DB_URL", "memory://")
	if got, want := resolveDSN(), "memory://"; got != want {
		t.Errorf("resolveDSN = %q, want %q (URL env should win even when DB_PATH is set)", got, want)
	}

	t.Setenv("MNEMOS_DB_URL", "sqlite:///srv/cogstack.db?namespace=mnemos")
	if got := resolveDSN(); got != "sqlite:///srv/cogstack.db?namespace=mnemos" {
		t.Errorf("resolveDSN dropped query parameters: got %q", got)
	}
}

// TestResolveDSN_FallsBackToSQLiteFromPath verifies the legacy
// migration path: with no MNEMOS_DB_URL, we must wrap the resolved
// SQLite file path as sqlite://<path> so the registry can dispatch.
func TestResolveDSN_FallsBackToSQLiteFromPath(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("MNEMOS_DB_URL", "")
	override := filepath.Join(root, "legacy.db")
	t.Setenv("MNEMOS_DB_PATH", override)
	t.Chdir(root)

	want := "sqlite://" + override
	if got := resolveDSN(); got != want {
		t.Errorf("resolveDSN = %q, want %q", got, want)
	}
}

// TestResolveDSN_ProjectFallback covers the case where neither env
// var is set but a .mnemos/ directory exists: we should still
// produce a usable sqlite:// DSN pointing at the project DB.
func TestResolveDSN_ProjectFallback(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("MNEMOS_DB_URL", "")
	t.Setenv("MNEMOS_DB_PATH", "")
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "xdg"))
	if err := os.Mkdir(filepath.Join(root, ".mnemos"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	t.Chdir(root)

	want := "sqlite://" + filepath.Join(root, ".mnemos", "mnemos.db")
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
	t.Setenv("MNEMOS_DB_PATH", "")
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

// TestOpenConn_OpensSQLiteBackendFromLegacyPath mirrors
// TestOpenConn_OpensMemoryBackend but covers the migration default:
// users with only MNEMOS_DB_PATH set still get a working SQLite Conn.
func TestOpenConn_OpensSQLiteBackendFromLegacyPath(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("MNEMOS_DB_URL", "")
	t.Setenv("MNEMOS_DB_PATH", filepath.Join(root, "legacy.db"))
	t.Chdir(root)

	conn, err := openConn(context.Background())
	if err != nil {
		t.Fatalf("openConn(sqlite legacy): %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	if conn.Raw == nil {
		t.Error("expected SQLite Conn to expose *sql.DB through Raw")
	}
}
