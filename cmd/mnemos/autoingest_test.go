package main

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/felixgeelhaar/mnemos/internal/store/sqlite"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestDiscoverProjectDocs_FindsStandardRootFiles(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "README.md"), "# Project")
	writeFile(t, filepath.Join(root, "PRD.md"), "PRD")
	writeFile(t, filepath.Join(root, "CHANGELOG.md"), "changes")
	writeFile(t, filepath.Join(root, "Roadmap.md"), "roadmap")
	writeFile(t, filepath.Join(root, "CLAUDE.md"), "claude")
	writeFile(t, filepath.Join(root, "src", "main.go"), "package main") // should be ignored
	writeFile(t, filepath.Join(root, "random.md"), "noise")              // should be ignored

	got := discoverProjectDocs(root)
	want := []string{
		filepath.Join(root, "CHANGELOG.md"),
		filepath.Join(root, "CLAUDE.md"),
		filepath.Join(root, "PRD.md"),
		filepath.Join(root, "README.md"),
		filepath.Join(root, "Roadmap.md"),
	}
	sort.Strings(want)
	if !equalSlices(got, want) {
		t.Fatalf("discoverProjectDocs = %v, want %v", got, want)
	}
}

func TestDiscoverProjectDocs_CaseInsensitiveBasenames(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "readme.md"), "lowercase")
	writeFile(t, filepath.Join(root, "Prd.MD"), "mixed")

	got := discoverProjectDocs(root)
	if len(got) != 2 {
		t.Fatalf("expected 2 docs, got %d (%v)", len(got), got)
	}
}

func TestDiscoverProjectDocs_PicksUpDocsDirectoryOneLevel(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "docs", "phase2-plan.md"), "plan")
	writeFile(t, filepath.Join(root, "docs", "backlog.md"), "backlog")
	writeFile(t, filepath.Join(root, "docs", "deep", "skip-me.md"), "should be ignored") // not recursive

	got := discoverProjectDocs(root)
	want := []string{
		filepath.Join(root, "docs", "backlog.md"),
		filepath.Join(root, "docs", "phase2-plan.md"),
	}
	sort.Strings(want)
	if !equalSlices(got, want) {
		t.Fatalf("discoverProjectDocs = %v, want %v", got, want)
	}
}

func TestDiscoverProjectDocs_RecursesIntoAdrSubdirs(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "docs", "adr", "0001-use-sqlite.md"), "ADR")
	writeFile(t, filepath.Join(root, "docs", "adr", "2026", "0002-bm25.md"), "ADR nested")
	writeFile(t, filepath.Join(root, "adr", "0003-mcp.md"), "ADR top-level")

	got := discoverProjectDocs(root)
	want := []string{
		filepath.Join(root, "adr", "0003-mcp.md"),
		filepath.Join(root, "docs", "adr", "0001-use-sqlite.md"),
		filepath.Join(root, "docs", "adr", "2026", "0002-bm25.md"),
	}
	sort.Strings(want)
	if !equalSlices(got, want) {
		t.Fatalf("discoverProjectDocs = %v, want %v", got, want)
	}
}

func TestDiscoverProjectDocs_EmptyRoot(t *testing.T) {
	root := t.TempDir()
	if got := discoverProjectDocs(root); len(got) != 0 {
		t.Fatalf("expected no docs in empty root, got %v", got)
	}
}

func TestExistingSourcePaths_LoadsFromMetadata(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "mnemos.db")
	db, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	ctx := context.Background()
	if _, err := db.ExecContext(ctx,
		`INSERT INTO events (id, run_id, schema_version, content, source_input_id, timestamp, metadata_json, ingested_at) VALUES
		 ('e1', 'r1', 'v1', 'c', 'in1', '2026-04-17T00:00:00Z', '{"input_source_path":"/a/README.md"}', '2026-04-17T00:00:00Z'),
		 ('e2', 'r1', 'v1', 'c', 'in2', '2026-04-17T00:00:00Z', '{"input_source_path":"/a/PRD.md"}',    '2026-04-17T00:00:00Z'),
		 ('e3', 'r1', 'v1', 'c', 'in3', '2026-04-17T00:00:00Z', '{"input_source_path":"/a/README.md"}', '2026-04-17T00:00:00Z'),
		 ('e4', 'r1', 'v1', 'c', 'in4', '2026-04-17T00:00:00Z', '{"source":"raw_text"}',                '2026-04-17T00:00:00Z')`,
	); err != nil {
		t.Fatalf("insert: %v", err)
	}

	got, err := existingSourcePaths(ctx, db)
	if err != nil {
		t.Fatalf("existingSourcePaths: %v", err)
	}
	want := map[string]struct{}{
		"/a/README.md": {},
		"/a/PRD.md":    {},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d entries, want %d (%v)", len(got), len(want), got)
	}
	for k := range want {
		if _, ok := got[k]; !ok {
			t.Errorf("missing %q in result", k)
		}
	}
}

func TestAutoIngestProjectDocs_IngestsThenDedupesOnSecondRun(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "README.md"), "We use SQLite for storage. The pipeline runs ingest, extract, and relate.")
	writeFile(t, filepath.Join(root, "PRD.md"), "The product targets AI engineers. The system must be local-first.")

	dbPath := filepath.Join(t.TempDir(), "mnemos.db")
	db, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	ctx := context.Background()

	ingested, skipped := autoIngestProjectDocs(ctx, db, root)
	if ingested != 2 || skipped != 0 {
		t.Fatalf("first run: ingested=%d skipped=%d, want 2 and 0", ingested, skipped)
	}

	var eventCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM events`).Scan(&eventCount); err != nil {
		t.Fatalf("count events: %v", err)
	}
	if eventCount == 0 {
		t.Fatal("expected events persisted, got 0")
	}

	ingested2, skipped2 := autoIngestProjectDocs(ctx, db, root)
	if ingested2 != 0 || skipped2 != 2 {
		t.Fatalf("second run: ingested=%d skipped=%d, want 0 and 2", ingested2, skipped2)
	}

	var eventCount2 int
	if err := db.QueryRow(`SELECT COUNT(*) FROM events`).Scan(&eventCount2); err != nil {
		t.Fatalf("count events second: %v", err)
	}
	if eventCount2 != eventCount {
		t.Fatalf("event count changed across runs: was %d, now %d", eventCount, eventCount2)
	}
}

func TestAutoIngestProjectDocs_NoDocsReturnsZero(t *testing.T) {
	root := t.TempDir() // empty
	dbPath := filepath.Join(t.TempDir(), "mnemos.db")
	db, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	ingested, skipped := autoIngestProjectDocs(context.Background(), db, root)
	if ingested != 0 || skipped != 0 {
		t.Fatalf("ingested=%d skipped=%d, want 0 and 0", ingested, skipped)
	}
}

func equalSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
