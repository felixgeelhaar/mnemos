package main

import (
	"encoding/hex"
	"os"
	"testing"
)

// TestMain pins a deterministic JWT signing secret for the whole test
// binary. Without it, each test that calls newServerMux would fall back
// to auth.LoadOrCreateSecret's file path and write `jwt-secret` into
// the developer's ~/.mnemos directory — a test-pollution bug we don't
// want. Individual tests that need a specific secret can still override
// with t.Setenv inside the test.
func TestMain(m *testing.M) {
	if os.Getenv("MNEMOS_JWT_SECRET") == "" {
		secret := make([]byte, 32)
		for i := range secret {
			secret[i] = byte(i)
		}
		_ = os.Setenv("MNEMOS_JWT_SECRET", hex.EncodeToString(secret))
	}
	os.Exit(m.Run())
}
