package auth

import (
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// LoadOrCreateSecret resolves the JWT signing secret in this order:
//  1. MNEMOS_JWT_SECRET env var (hex-encoded; useful in CI/Docker)
//  2. The file at path, hex-decoded (auto-created with 32 random bytes
//     if it doesn't exist)
//
// Returns the secret bytes plus a bool that is true when the file was
// auto-created — callers can print a warning so the operator knows a
// new secret was generated and that any previously-issued tokens are
// invalidated.
func LoadOrCreateSecret(path string) ([]byte, bool, error) {
	if envHex := strings.TrimSpace(os.Getenv("MNEMOS_JWT_SECRET")); envHex != "" {
		b, err := hex.DecodeString(envHex)
		if err != nil {
			return nil, false, fmt.Errorf("MNEMOS_JWT_SECRET is not valid hex: %w", err)
		}
		if len(b) < 32 {
			return nil, false, errors.New("MNEMOS_JWT_SECRET must decode to at least 32 bytes")
		}
		return b, false, nil
	}

	if path == "" {
		return nil, false, errors.New("no secret path provided and MNEMOS_JWT_SECRET unset")
	}

	if data, err := os.ReadFile(path); err == nil { //nolint:gosec // G304: server-resolved path
		decoded, err := hex.DecodeString(strings.TrimSpace(string(data)))
		if err != nil {
			return nil, false, fmt.Errorf("read %s: not hex-encoded: %w", path, err)
		}
		if len(decoded) < 32 {
			return nil, false, fmt.Errorf("read %s: must decode to at least 32 bytes", path)
		}
		return decoded, false, nil
	}

	// Generate.
	secret, err := GenerateSecret()
	if err != nil {
		return nil, false, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, false, fmt.Errorf("create secret dir: %w", err)
	}
	if err := os.WriteFile(path, []byte(hex.EncodeToString(secret)), 0o600); err != nil {
		return nil, false, fmt.Errorf("write secret to %s: %w", path, err)
	}
	return secret, true, nil
}

// DefaultSecretPath returns the default location for the JWT secret
// file. Project-scoped (.mnemos/) when a project DB is in use, otherwise
// XDG-equivalent under the user's home.
func DefaultSecretPath(projectRoot string) string {
	if projectRoot != "" {
		return filepath.Join(projectRoot, ".mnemos", "jwt-secret")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".mnemos", "jwt-secret")
}
