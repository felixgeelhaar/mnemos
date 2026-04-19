package auth

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadOrCreateSecret_GeneratesWhenMissing(t *testing.T) {
	t.Setenv("MNEMOS_JWT_SECRET", "")
	dir := t.TempDir()
	path := filepath.Join(dir, "subdir", "jwt-secret")

	got, created, err := LoadOrCreateSecret(path)
	if err != nil {
		t.Fatalf("LoadOrCreateSecret: %v", err)
	}
	if !created {
		t.Error("expected created=true on first call")
	}
	if len(got) != 32 {
		t.Errorf("secret length = %d, want 32", len(got))
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("secret file not written: %v", err)
	}
}

func TestLoadOrCreateSecret_ReadsExistingFile(t *testing.T) {
	t.Setenv("MNEMOS_JWT_SECRET", "")
	dir := t.TempDir()
	path := filepath.Join(dir, "jwt-secret")

	first, _, err := LoadOrCreateSecret(path)
	if err != nil {
		t.Fatalf("first: %v", err)
	}

	second, created, err := LoadOrCreateSecret(path)
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if created {
		t.Error("created=true on second call (should re-read)")
	}
	if hex.EncodeToString(first) != hex.EncodeToString(second) {
		t.Error("second read returned different bytes")
	}
}

func TestLoadOrCreateSecret_EnvOverridesFile(t *testing.T) {
	envSecret := strings.Repeat("ab", 32) // 32 bytes hex = 32 bytes decoded
	t.Setenv("MNEMOS_JWT_SECRET", envSecret)
	dir := t.TempDir()
	path := filepath.Join(dir, "jwt-secret")

	got, created, err := LoadOrCreateSecret(path)
	if err != nil {
		t.Fatalf("LoadOrCreateSecret: %v", err)
	}
	if created {
		t.Error("created=true with env set (file should not be touched)")
	}
	if hex.EncodeToString(got) != envSecret {
		t.Error("env value not honored")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("file was written despite env override")
	}
}

func TestLoadOrCreateSecret_RejectsTooShortEnv(t *testing.T) {
	t.Setenv("MNEMOS_JWT_SECRET", "abcd") // 2 bytes decoded
	if _, _, err := LoadOrCreateSecret(""); err == nil {
		t.Fatal("expected rejection for short env secret")
	}
}

func TestDefaultSecretPath_ProjectVsHome(t *testing.T) {
	if got := DefaultSecretPath("/some/project"); got != "/some/project/.mnemos/jwt-secret" {
		t.Errorf("project path = %q", got)
	}
	t.Setenv("HOME", "/test/home")
	if got := DefaultSecretPath(""); got != "/test/home/.mnemos/jwt-secret" {
		t.Errorf("home path = %q", got)
	}
}
