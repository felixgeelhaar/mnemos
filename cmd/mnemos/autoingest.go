package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/felixgeelhaar/mnemos/internal/domain"
	"github.com/felixgeelhaar/mnemos/internal/embedding"
	"github.com/felixgeelhaar/mnemos/internal/ingest"
	"github.com/felixgeelhaar/mnemos/internal/parser"
	"github.com/felixgeelhaar/mnemos/internal/pipeline"
	"github.com/felixgeelhaar/mnemos/internal/relate"
	"github.com/felixgeelhaar/mnemos/internal/store/sqlite"
)

// generateEmbeddingsBestEffort creates event and claim embeddings when an
// embedding provider is configured. When no provider is configured (no
// Ollama, no MNEMOS_EMBED_PROVIDER), it silently no-ops so that auto-ingest
// still works in zero-config environments. Any actual provider call failure
// is logged to stderr but does not propagate — persisted events and claims
// remain queryable via token-overlap fallback. Embeddings are derived
// data; re-running this on the same events/claims is safe and overwrites.
//
// Each batch is attempted independently: a flaky event batch should not
// stop us from trying claims, since the underlying provider call may
// succeed for one shape and not the other (e.g., long-content
// truncation rules differ).
func generateEmbeddingsBestEffort(ctx context.Context, db *sql.DB, events []domain.Event, claims []domain.Claim) {
	if _, err := embedding.ConfigFromEnv(); err != nil {
		return
	}
	if len(events) > 0 {
		if _, err := pipeline.GenerateEmbeddings(ctx, db, events); err != nil {
			fmt.Fprintf(os.Stderr, "embeddings: event batch failed: %v\n", err)
		}
	}
	if len(claims) > 0 {
		if _, err := pipeline.GenerateClaimEmbeddings(ctx, db, claims); err != nil {
			fmt.Fprintf(os.Stderr, "embeddings: claim batch failed: %v\n", err)
		}
	}
}

// rootDocBasenames are exact (case-insensitive) filenames at the project root
// that auto-ingest will pick up.
var rootDocBasenames = []string{
	"README.md", "README.markdown",
	"PRD.md",
	"CHANGELOG.md",
	"ROADMAP.md", "Roadmap.md",
	"CLAUDE.md",
	"ARCHITECTURE.md",
}

// docDirs are subdirectories whose top-level .md files auto-ingest will pick
// up (one level deep — not recursive). Plus their adr/ subdirectories.
var docDirs = []string{"docs", "doc"}

// adrSubDirs are walked recursively for .md files (ADRs commonly nest by
// year or topic).
var adrSubDirs = []string{"adr", "decisions", filepath.Join("docs", "adr"), filepath.Join("docs", "decisions")}

// discoverProjectDocs returns the absolute paths of standard project documents
// found under root. Results are deduplicated and sorted for stable ordering.
func discoverProjectDocs(root string) []string {
	seen := make(map[string]struct{})
	var paths []string

	add := func(p string) {
		abs, err := filepath.Abs(p)
		if err != nil {
			return
		}
		if _, dup := seen[abs]; dup {
			return
		}
		seen[abs] = struct{}{}
		paths = append(paths, abs)
	}

	rootEntries, err := os.ReadDir(root)
	if err == nil {
		basenameSet := make(map[string]struct{}, len(rootDocBasenames))
		for _, b := range rootDocBasenames {
			basenameSet[strings.ToLower(b)] = struct{}{}
		}
		for _, e := range rootEntries {
			if e.IsDir() {
				continue
			}
			if _, ok := basenameSet[strings.ToLower(e.Name())]; ok {
				add(filepath.Join(root, e.Name()))
			}
		}
	}

	for _, sub := range docDirs {
		dir := filepath.Join(root, sub)
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			if strings.EqualFold(filepath.Ext(e.Name()), ".md") {
				add(filepath.Join(dir, e.Name()))
			}
		}
	}

	for _, sub := range adrSubDirs {
		dir := filepath.Join(root, sub)
		_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				return nil
			}
			if strings.EqualFold(filepath.Ext(d.Name()), ".md") {
				add(path)
			}
			return nil
		})
	}

	sort.Strings(paths)
	return paths
}

// existingSourcePaths returns the set of absolute file paths that have already
// been ingested into db (extracted from event metadata via JSON1).
func existingSourcePaths(ctx context.Context, db *sql.DB) (map[string]struct{}, error) {
	const q = `SELECT DISTINCT json_extract(metadata_json, '$.input_source_path') FROM events WHERE json_extract(metadata_json, '$.input_source_path') IS NOT NULL`
	rows, err := db.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("query existing source paths: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make(map[string]struct{})
	for rows.Next() {
		var p sql.NullString
		if err := rows.Scan(&p); err != nil {
			return nil, fmt.Errorf("scan source path: %w", err)
		}
		if p.Valid && p.String != "" {
			out[p.String] = struct{}{}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate source paths: %w", err)
	}
	return out, nil
}

// AutoIngestReport summarises an auto-ingest pass. Counts cover the
// happy path; PerFileErrors lists the path → error pairs for any
// document that failed mid-pipeline, so callers can surface a real
// summary rather than relying on stderr scraping.
type AutoIngestReport struct {
	Ingested       int
	Skipped        int
	PerFileErrors  map[string]error
	DedupeFailed   bool  // existing-path lookup failed; ingest may produce duplicate runs
	ExtractorError error // pipeline.NewExtractor failed; nothing was attempted
}

// HasFailures reports whether any per-file or pre-flight error
// occurred. Callers in --strict mode use this to fail loudly;
// best-effort callers (MCP startup) just log the report.
func (r AutoIngestReport) HasFailures() bool {
	return r.ExtractorError != nil || r.DedupeFailed || len(r.PerFileErrors) > 0
}

// autoIngestProjectDocs scans root for standard project documents and
// ingests any that haven't been seen yet. Uses rule-based extraction
// for speed — users can re-process specific files via the MCP
// process_text tool with useLlm=true if they want LLM-quality claims.
//
// Returns a structured report so callers can decide whether to treat
// failures as fatal. The per-doc loop continues past per-file errors
// (one bad file shouldn't block the rest of the project), but
// preflight errors that affect all docs (extractor build failure,
// dedupe lookup failure) are surfaced rather than silently skipped.
func autoIngestProjectDocs(ctx context.Context, db *sql.DB, root, actor string) AutoIngestReport {
	report := AutoIngestReport{PerFileErrors: map[string]error{}}

	docs := discoverProjectDocs(root)
	if len(docs) == 0 {
		return report
	}

	existing, err := existingSourcePaths(ctx, db)
	if err != nil {
		// Treating this as recoverable would silently re-ingest every
		// doc on every restart, creating duplicate runs. Surface it.
		fmt.Fprintf(os.Stderr, "auto-ingest: failed to query existing sources: %v\n", err)
		report.DedupeFailed = true
		return report
	}

	service := ingest.NewService()
	normalizer := parser.NewNormalizer()
	extractor, err := pipeline.NewExtractor(false)
	if err != nil {
		fmt.Fprintf(os.Stderr, "auto-ingest: failed to build extractor: %v\n", err)
		report.ExtractorError = err
		return report
	}
	relEngine := relate.NewEngine()

	runID := fmt.Sprintf("auto-ingest-%s", time.Now().UTC().Format("20060102T150405"))

	for _, path := range docs {
		if _, seen := existing[path]; seen {
			report.Skipped++
			continue
		}
		if err := ingestSingleDoc(ctx, db, service, normalizer, extractor, relEngine, runID, path, actor); err != nil {
			fmt.Fprintf(os.Stderr, "auto-ingest: %s: %v\n", path, err)
			report.PerFileErrors[path] = err
			continue
		}
		report.Ingested++
	}

	return report
}

func ingestSingleDoc(
	ctx context.Context,
	db *sql.DB,
	service ingest.Service,
	normalizer parser.Normalizer,
	extractor *pipeline.Extractor,
	relEngine relate.Engine,
	runID string,
	path string,
	actor string,
) error {
	input, content, err := service.IngestFile(path)
	if err != nil {
		return fmt.Errorf("ingest: %w", err)
	}
	events, err := normalizer.Normalize(input, content)
	if err != nil {
		return fmt.Errorf("normalize: %w", err)
	}
	for i := range events {
		events[i].RunID = runID
	}
	claims, links, err := extractor.ExtractFn(events)
	if err != nil {
		return fmt.Errorf("extract: %w", err)
	}
	rels, err := relEngine.Detect(claims)
	if err != nil {
		return fmt.Errorf("relate: %w", err)
	}

	claimRepo := sqlite.NewClaimRepository(db)
	existingClaims, err := claimRepo.ListAll(ctx)
	if err != nil {
		return fmt.Errorf("list existing claims: %w", err)
	}
	if len(existingClaims) > 0 {
		incremental, err := relEngine.DetectIncremental(claims, existingClaims)
		if err != nil {
			return fmt.Errorf("incremental relate: %w", err)
		}
		rels = append(rels, incremental...)
	}

	stampEventActor(events, actor)
	stampClaimActor(claims, actor)
	stampRelationshipActor(rels, actor)
	if err := pipeline.PersistArtifacts(ctx, db, events, claims, links, rels); err != nil {
		return fmt.Errorf("persist: %w", err)
	}
	generateEmbeddingsBestEffort(ctx, db, events, claims)
	return nil
}
