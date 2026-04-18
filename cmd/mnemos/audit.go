package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/felixgeelhaar/mnemos/internal/store/sqlite"
)

// auditExport is the on-the-wire shape of `mnemos audit`. Top-level fields
// are explicitly listed so a downstream compliance tool can validate the
// document against a schema. The schema_version lets future changes break
// backwards compatibility deliberately rather than silently.
type auditExport struct {
	SchemaVersion string              `json:"schema_version"`
	GeneratedAt   string              `json:"generated_at"`
	DBPath        string              `json:"db_path"`
	Counts        auditCounts         `json:"counts"`
	Events        []eventDTO          `json:"events"`
	Claims        []claimDTO          `json:"claims"`
	Evidence      []claimEvidenceItem `json:"evidence"`
	Relationships []relationshipDTO   `json:"relationships"`
	Embeddings    []embeddingDTO      `json:"embeddings,omitempty"`
}

type auditCounts struct {
	Events        int `json:"events"`
	Claims        int `json:"claims"`
	Evidence      int `json:"evidence"`
	Relationships int `json:"relationships"`
	Embeddings    int `json:"embeddings"`
}

const auditSchemaVersion = "audit.v1"

// handleAudit dumps the entire knowledge base to stdout as a single JSON
// document. Intended for compliance reviews, point-in-time backups, and
// debugging. Embeddings are included only when --include-embeddings is
// passed because their float arrays inflate the output by an order of
// magnitude and most audit consumers don't need them.
func handleAudit(args []string, _ Flags) {
	includeEmbeddings := false
	for _, a := range args {
		switch a {
		case "--include-embeddings":
			includeEmbeddings = true
		default:
			exitWithMnemosError(false, NewUserError("unknown audit flag %q", a))
			return
		}
	}

	dbPath := resolveDBPath()
	db, err := sqlite.Open(dbPath)
	if err != nil {
		exitWithMnemosError(false, NewSystemError(err, "open database"))
		return
	}
	defer closeDB(db)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	export, err := buildAuditExport(ctx, db, dbPath, includeEmbeddings)
	if err != nil {
		exitWithMnemosError(false, NewSystemError(err, "build audit export"))
		return
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(export); err != nil {
		exitWithMnemosError(false, NewSystemError(err, "encode audit export"))
		return
	}
}

func buildAuditExport(ctx context.Context, db *sql.DB, dbPath string, includeEmbeddings bool) (auditExport, error) {
	events, err := loadAllEventsForPush(ctx, db)
	if err != nil {
		return auditExport{}, fmt.Errorf("load events: %w", err)
	}
	claims, evidence, err := loadAllClaimsForPush(ctx, db)
	if err != nil {
		return auditExport{}, fmt.Errorf("load claims: %w", err)
	}
	rels, err := loadAllRelationshipsForPush(ctx, db)
	if err != nil {
		return auditExport{}, fmt.Errorf("load relationships: %w", err)
	}

	export := auditExport{
		SchemaVersion: auditSchemaVersion,
		GeneratedAt:   time.Now().UTC().Format(time.RFC3339),
		DBPath:        dbPath,
		Events:        events,
		Claims:        claims,
		Evidence:      evidence,
		Relationships: rels,
		Counts: auditCounts{
			Events:        len(events),
			Claims:        len(claims),
			Evidence:      len(evidence),
			Relationships: len(rels),
		},
	}

	if includeEmbeddings {
		embs, err := loadAllEmbeddingsForPush(ctx, db)
		if err != nil {
			return auditExport{}, fmt.Errorf("load embeddings: %w", err)
		}
		export.Embeddings = embs
		export.Counts.Embeddings = len(embs)
	}

	return export, nil
}
