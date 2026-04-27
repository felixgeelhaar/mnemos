package postgres_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/felixgeelhaar/mnemos/internal/store"
	"github.com/felixgeelhaar/mnemos/internal/store/postgres"
)

func TestParseDSN_DefaultsNamespaceToMnemos(t *testing.T) {
	t.Parallel()
	d, err := postgres.ParseDSN("postgres://user:pw@host:5432/cogstack")
	if err != nil {
		t.Fatalf("ParseDSN: %v", err)
	}
	if d.Namespace != "mnemos" {
		t.Errorf("Namespace = %q, want mnemos (default)", d.Namespace)
	}
	// LibpqDSN should equal Raw when there was no namespace param to strip.
	if d.LibpqDSN != "postgres://user:pw@host:5432/cogstack" {
		t.Errorf("LibpqDSN = %q, want raw passthrough", d.LibpqDSN)
	}
}

func TestParseDSN_StripsNamespaceFromQuery(t *testing.T) {
	t.Parallel()
	d, err := postgres.ParseDSN("postgres://user:pw@host/cogstack?sslmode=require&namespace=team_x")
	if err != nil {
		t.Fatalf("ParseDSN: %v", err)
	}
	if d.Namespace != "team_x" {
		t.Errorf("Namespace = %q, want team_x", d.Namespace)
	}
	if strings.Contains(d.LibpqDSN, "namespace=") {
		t.Errorf("LibpqDSN should not contain namespace=, got %q", d.LibpqDSN)
	}
	if !strings.Contains(d.LibpqDSN, "sslmode=require") {
		t.Errorf("LibpqDSN dropped non-namespace params: %q", d.LibpqDSN)
	}
}

func TestParseDSN_AcceptsPostgresqlScheme(t *testing.T) {
	t.Parallel()
	d, err := postgres.ParseDSN("postgresql://user:pw@host/cogstack")
	if err != nil {
		t.Fatalf("ParseDSN(postgresql://...): %v", err)
	}
	if d.Namespace != "mnemos" {
		t.Errorf("Namespace = %q, want mnemos", d.Namespace)
	}
}

func TestParseDSN_RejectsInvalidNamespace(t *testing.T) {
	t.Parallel()
	bad := []string{
		"postgres://h/d?namespace=Team-X", // hyphen + capitals
		"postgres://h/d?namespace=1team",  // starts with digit
		"postgres://h/d?namespace=",       // empty after = → defaults; not invalid
		"postgres://h/d?namespace=very_very_long_name_that_exceeds_the_postgres_identifier_limit_of_63_chars",
	}
	for _, dsn := range bad {
		_, err := postgres.ParseDSN(dsn)
		if dsn == "postgres://h/d?namespace=" {
			// Empty defaults to mnemos; should NOT error.
			if err != nil {
				t.Errorf("empty namespace should default, got %v", err)
			}
			continue
		}
		if err == nil {
			t.Errorf("ParseDSN(%q) accepted invalid namespace", dsn)
		}
	}
}

func TestParseDSN_RejectsNonPostgresScheme(t *testing.T) {
	t.Parallel()
	if _, err := postgres.ParseDSN("sqlite:///x.db"); err == nil {
		t.Error("ParseDSN accepted sqlite:// scheme")
	}
}

// TestStoreOpen_ReturnsNotImplemented documents the scaffold contract:
// while the provider is in development, store.Open with a postgres://
// DSN must return ErrNotImplemented (wrapped) rather than crashing or
// claiming success. Operators see a clear, actionable error.
func TestStoreOpen_ReturnsNotImplemented(t *testing.T) {
	t.Parallel()
	_, err := store.Open(context.Background(), "postgres://user:pw@localhost/cogstack")
	if err == nil {
		t.Fatal("expected error from scaffold provider, got nil")
	}
	if !errors.Is(err, postgres.ErrNotImplemented) {
		t.Errorf("error chain missing ErrNotImplemented: %v", err)
	}
}

func TestSupportedSchemes_IncludesPostgres(t *testing.T) {
	t.Parallel()
	got := store.SupportedSchemes()
	want := map[string]bool{"postgres": false, "postgresql": false}
	for _, s := range got {
		if _, ok := want[s]; ok {
			want[s] = true
		}
	}
	for s, seen := range want {
		if !seen {
			t.Errorf("SupportedSchemes missing %q (got %v)", s, got)
		}
	}
}
