package sqlite

import (
	"path/filepath"
	"testing"
)

func TestParseDSN_DefaultNamespacePreservesPath(t *testing.T) {
	d, err := parseDSN("sqlite:///data/mnemos.db")
	if err != nil {
		t.Fatalf("parseDSN: %v", err)
	}
	if d.Path != "/data/mnemos.db" {
		t.Errorf("Path = %q, want /data/mnemos.db", d.Path)
	}
	if d.Namespace != "mnemos" {
		t.Errorf("Namespace = %q, want mnemos", d.Namespace)
	}
}

func TestParseDSN_ExplicitDefaultNamespacePreservesPath(t *testing.T) {
	d, err := parseDSN("sqlite:///data/mnemos.db?namespace=mnemos")
	if err != nil {
		t.Fatalf("parseDSN: %v", err)
	}
	if d.Path != "/data/mnemos.db" {
		t.Errorf("Path = %q, want /data/mnemos.db", d.Path)
	}
}

func TestParseDSN_NonDefaultNamespaceModifiesPath(t *testing.T) {
	d, err := parseDSN("sqlite:///data/cogstack.db?namespace=chronos")
	if err != nil {
		t.Fatalf("parseDSN: %v", err)
	}
	want := filepath.FromSlash("/data/cogstack_chronos.db")
	if d.Path != want {
		t.Errorf("Path = %q, want %q", d.Path, want)
	}
	if d.Namespace != "chronos" {
		t.Errorf("Namespace = %q, want chronos", d.Namespace)
	}
}

func TestParseDSN_NonDefaultNamespaceNoExtension(t *testing.T) {
	d, err := parseDSN("sqlite:///data/db?namespace=nous")
	if err != nil {
		t.Fatalf("parseDSN: %v", err)
	}
	want := filepath.FromSlash("/data/db_nous")
	if d.Path != want {
		t.Errorf("Path = %q, want %q", d.Path, want)
	}
}

func TestParseDSN_RejectsInvalidNamespace(t *testing.T) {
	for _, bad := range []string{
		"sqlite:///data/db?namespace=Team-X",
		"sqlite:///data/db?namespace=1team",
		"sqlite:///data/db?namespace=very_very_long_name_that_exceeds_the_postgres_identifier_limit_of_63_chars",
	} {
		if _, err := parseDSN(bad); err == nil {
			t.Errorf("parseDSN(%q) accepted invalid namespace", bad)
		}
	}
}

func TestParseDSN_Sqlite3Scheme(t *testing.T) {
	d, err := parseDSN("sqlite3:///data/db.db?namespace=chronos")
	if err != nil {
		t.Fatalf("parseDSN: %v", err)
	}
	want := filepath.FromSlash("/data/db_chronos.db")
	if d.Path != want {
		t.Errorf("Path = %q, want %q", d.Path, want)
	}
}
