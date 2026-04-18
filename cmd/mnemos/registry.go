package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/felixgeelhaar/mnemos/internal/embedding"
	"github.com/felixgeelhaar/mnemos/internal/store/sqlite"
)

const (
	registryConfigName    = "config.json"
	pushBatchSize         = 100
	pullPageSize          = 100
	registryHTTPTimeout   = 60 * time.Second
	provenanceRegistryKey = "pulled_from_registry"
	provenancePulledAtKey = "pulled_at"
)

// registryConfig is the persisted shape of .mnemos/config.json. Only the
// registry block is populated today; the file is namespaced so future
// settings (preferred LLM, default scope, etc.) can sit alongside.
type registryConfig struct {
	Registry registrySettings `json:"registry"`
}

type registrySettings struct {
	URL   string `json:"url,omitempty"`
	Token string `json:"token,omitempty"`
}

// resolveRegistry returns the registry URL and token for the current project,
// merging (in order of precedence): CLI flags > env vars > project config.
// Returns an error only when no source provides a URL.
func resolveRegistry(flagURL, flagToken string) (string, string, error) {
	regURL := strings.TrimSpace(flagURL)
	token := flagToken

	if regURL == "" {
		regURL = strings.TrimSpace(os.Getenv("MNEMOS_REGISTRY_URL"))
	}
	if token == "" {
		token = os.Getenv("MNEMOS_REGISTRY_TOKEN")
	}

	if regURL == "" {
		if cfg, err := loadProjectConfig(); err == nil {
			regURL = strings.TrimSpace(cfg.Registry.URL)
			if token == "" {
				token = cfg.Registry.Token
			}
		}
	}

	if regURL == "" {
		return "", "", fmt.Errorf("no registry URL configured — set MNEMOS_REGISTRY_URL, pass --url, or run 'mnemos registry connect <url>'")
	}
	if _, err := url.Parse(regURL); err != nil {
		return "", "", fmt.Errorf("invalid registry URL %q: %w", regURL, err)
	}
	return strings.TrimRight(regURL, "/"), token, nil
}

func projectConfigPath() (string, error) {
	_, root, ok := findProjectDB()
	if !ok {
		return "", fmt.Errorf("no project (.mnemos/) found — run 'mnemos init' first")
	}
	return filepath.Join(root, ".mnemos", registryConfigName), nil
}

func loadProjectConfig() (registryConfig, error) {
	p, err := projectConfigPath()
	if err != nil {
		return registryConfig{}, err
	}
	data, err := os.ReadFile(p) //nolint:gosec // G304: project config in user-owned .mnemos directory
	if err != nil {
		return registryConfig{}, err
	}
	var cfg registryConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return registryConfig{}, fmt.Errorf("parse %s: %w", p, err)
	}
	return cfg, nil
}

func saveProjectConfig(cfg registryConfig) (string, error) {
	p, err := projectConfigPath()
	if err != nil {
		return "", err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encode config: %w", err)
	}
	if err := os.WriteFile(p, data, 0o600); err != nil {
		return "", fmt.Errorf("write %s: %w", p, err)
	}
	return p, nil
}

// handleRegistry dispatches the `mnemos registry <subcommand>` family.
// Today only `connect` exists; later: `disconnect`, `status`, `set-token`.
func handleRegistry(args []string, _ Flags) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "error: registry requires a subcommand")
		fmt.Fprintln(os.Stderr, "  mnemos registry connect <url> [--token <token>]")
		os.Exit(int(ExitUsage))
	}
	switch args[0] {
	case "connect":
		handleRegistryConnect(args[1:])
	default:
		exitWithMnemosError(false, NewUserError("unknown registry subcommand %q", args[0]))
	}
}

func handleRegistryConnect(args []string) {
	if len(args) == 0 {
		exitWithMnemosError(false, NewUserError("registry connect requires a URL\n  mnemos registry connect <url> [--token <token>]"))
		return
	}
	regURL := args[0]
	args = args[1:]
	token := ""
	for i := 0; i < len(args); i++ {
		if args[i] == "--token" {
			if i+1 >= len(args) {
				exitWithMnemosError(false, NewUserError("--token requires a value"))
				return
			}
			token = args[i+1]
			i++
			continue
		}
		exitWithMnemosError(false, NewUserError("unknown flag %q", args[i]))
		return
	}
	if _, err := url.Parse(regURL); err != nil {
		exitWithMnemosError(false, NewUserError("invalid URL %q: %v", regURL, err))
		return
	}
	cfg := registryConfig{Registry: registrySettings{URL: strings.TrimRight(regURL, "/"), Token: token}}
	written, err := saveProjectConfig(cfg)
	if err != nil {
		exitWithMnemosError(false, NewSystemError(err, "save config"))
		return
	}
	fmt.Printf("connected to registry: %s\n", cfg.Registry.URL)
	fmt.Printf("config saved: %s\n", written)
	if token != "" {
		fmt.Println("auth: bearer token configured")
	} else {
		fmt.Println("auth: no token (registry must allow open writes)")
	}
}

// handlePush uploads all local events, claims, and relationships to the
// configured registry. Idempotent — registries upsert by ID, so re-running
// is safe. Reports counts per resource at the end.
func handlePush(args []string, _ Flags) {
	flagURL, flagToken := parseRegistryFlags(args)
	regURL, token, err := resolveRegistry(flagURL, flagToken)
	if err != nil {
		exitWithMnemosError(false, NewUserError("%s", err.Error()))
		return
	}

	dbPath := resolveDBPath()
	db, err := sqlite.Open(dbPath)
	if err != nil {
		exitWithMnemosError(false, NewSystemError(err, "open database"))
		return
	}
	defer closeDB(db)

	ctx, cancel := context.WithTimeout(context.Background(), registryHTTPTimeout*5)
	defer cancel()

	client := &http.Client{Timeout: registryHTTPTimeout}

	events, err := loadAllEventsForPush(ctx, db)
	if err != nil {
		exitWithMnemosError(false, NewSystemError(err, "load events"))
		return
	}
	claims, evidence, err := loadAllClaimsForPush(ctx, db)
	if err != nil {
		exitWithMnemosError(false, NewSystemError(err, "load claims"))
		return
	}
	rels, err := loadAllRelationshipsForPush(ctx, db)
	if err != nil {
		exitWithMnemosError(false, NewSystemError(err, "load relationships"))
		return
	}

	pushedEvents, err := pushBatched(ctx, client, regURL+"/v1/events", token, "events", batchToAny(eventsToBatches(events)))
	if err != nil {
		exitWithMnemosError(false, NewSystemError(err, "push events"))
		return
	}
	pushedClaims, err := pushBatched(ctx, client, regURL+"/v1/claims", token, "claims", batchToAny(claimsToBatches(claims, evidence)))
	if err != nil {
		exitWithMnemosError(false, NewSystemError(err, "push claims"))
		return
	}
	pushedRels, err := pushBatched(ctx, client, regURL+"/v1/relationships", token, "relationships", batchToAny(relsToBatches(rels)))
	if err != nil {
		exitWithMnemosError(false, NewSystemError(err, "push relationships"))
		return
	}
	embeddings, err := loadAllEmbeddingsForPush(ctx, db)
	if err != nil {
		exitWithMnemosError(false, NewSystemError(err, "load embeddings"))
		return
	}
	pushedEmbeddings, err := pushBatched(ctx, client, regURL+"/v1/embeddings", token, "embeddings", batchToAny(embeddingsToBatches(embeddings)))
	if err != nil {
		exitWithMnemosError(false, NewSystemError(err, "push embeddings"))
		return
	}

	fmt.Printf("pushed to %s\n", regURL)
	fmt.Printf("  events:        %d\n", pushedEvents)
	fmt.Printf("  claims:        %d\n", pushedClaims)
	fmt.Printf("  relationships: %d\n", pushedRels)
	fmt.Printf("  embeddings:    %d\n", pushedEmbeddings)
}

// handlePull downloads all events, claims, and relationships from the
// configured registry into the local database. Uses pagination and respects
// the registry's max page size (caps at 200). Idempotent.
func handlePull(args []string, _ Flags) {
	flagURL, flagToken := parseRegistryFlags(args)
	regURL, token, err := resolveRegistry(flagURL, flagToken)
	if err != nil {
		exitWithMnemosError(false, NewUserError("%s", err.Error()))
		return
	}

	dbPath := resolveDBPath()
	db, err := sqlite.Open(dbPath)
	if err != nil {
		exitWithMnemosError(false, NewSystemError(err, "open database"))
		return
	}
	defer closeDB(db)

	ctx, cancel := context.WithTimeout(context.Background(), registryHTTPTimeout*5)
	defer cancel()

	client := &http.Client{Timeout: registryHTTPTimeout}

	events, err := pullEvents(ctx, client, regURL, token)
	if err != nil {
		exitWithMnemosError(false, NewSystemError(err, "pull events"))
		return
	}
	stampPullProvenance(events, regURL, time.Now().UTC())
	claims, evidence, err := pullClaims(ctx, client, regURL, token)
	if err != nil {
		exitWithMnemosError(false, NewSystemError(err, "pull claims"))
		return
	}
	rels, err := pullRelationships(ctx, client, regURL, token)
	if err != nil {
		exitWithMnemosError(false, NewSystemError(err, "pull relationships"))
		return
	}

	insertedEvents, err := persistPulledEvents(ctx, db, events)
	if err != nil {
		exitWithMnemosError(false, NewSystemError(err, "persist events"))
		return
	}
	insertedClaims, err := persistPulledClaims(ctx, db, claims, evidence)
	if err != nil {
		exitWithMnemosError(false, NewSystemError(err, "persist claims"))
		return
	}
	insertedRels, err := persistPulledRelationships(ctx, db, rels)
	if err != nil {
		exitWithMnemosError(false, NewSystemError(err, "persist relationships"))
		return
	}
	embeddings, err := pullEmbeddings(ctx, client, regURL, token)
	if err != nil {
		exitWithMnemosError(false, NewSystemError(err, "pull embeddings"))
		return
	}
	insertedEmbeddings, err := persistPulledEmbeddings(ctx, db, embeddings)
	if err != nil {
		exitWithMnemosError(false, NewSystemError(err, "persist embeddings"))
		return
	}

	fmt.Printf("pulled from %s\n", regURL)
	fmt.Printf("  events:        %d\n", insertedEvents)
	fmt.Printf("  claims:        %d\n", insertedClaims)
	fmt.Printf("  relationships: %d\n", insertedRels)
	fmt.Printf("  embeddings:    %d\n", insertedEmbeddings)
}

func parseRegistryFlags(args []string) (string, string) {
	regURL := ""
	token := ""
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--url":
			if i+1 < len(args) {
				regURL = args[i+1]
				i++
			}
		case "--token":
			if i+1 < len(args) {
				token = args[i+1]
				i++
			}
		}
	}
	return regURL, token
}

func loadAllEventsForPush(ctx context.Context, db *sql.DB) ([]eventDTO, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, run_id, schema_version, content, source_input_id, timestamp, metadata_json, ingested_at FROM events`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var events []eventDTO
	for rows.Next() {
		var (
			e        eventDTO
			metaJSON string
		)
		if err := rows.Scan(&e.ID, &e.RunID, &e.SchemaVersion, &e.Content, &e.SourceInputID, &e.Timestamp, &metaJSON, &e.IngestedAt); err != nil {
			return nil, err
		}
		e.Metadata = map[string]string{}
		_ = json.Unmarshal([]byte(metaJSON), &e.Metadata)
		events = append(events, e)
	}
	return events, rows.Err()
}

func loadAllClaimsForPush(ctx context.Context, db *sql.DB) ([]claimDTO, []claimEvidenceItem, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, text, type, confidence, status, created_at FROM claims`)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = rows.Close() }()
	var claims []claimDTO
	for rows.Next() {
		var c claimDTO
		if err := rows.Scan(&c.ID, &c.Text, &c.Type, &c.Confidence, &c.Status, &c.CreatedAt); err != nil {
			return nil, nil, err
		}
		claims = append(claims, c)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}

	evRows, err := db.QueryContext(ctx, `SELECT claim_id, event_id FROM claim_evidence`)
	if err != nil {
		return claims, nil, err
	}
	defer func() { _ = evRows.Close() }()
	var evidence []claimEvidenceItem
	for evRows.Next() {
		var item claimEvidenceItem
		if err := evRows.Scan(&item.ClaimID, &item.EventID); err != nil {
			return claims, nil, err
		}
		evidence = append(evidence, item)
	}
	return claims, evidence, evRows.Err()
}

func loadAllRelationshipsForPush(ctx context.Context, db *sql.DB) ([]relationshipDTO, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, type, from_claim_id, to_claim_id, created_at FROM relationships`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var rels []relationshipDTO
	for rows.Next() {
		var r relationshipDTO
		if err := rows.Scan(&r.ID, &r.Type, &r.FromClaimID, &r.ToClaimID, &r.CreatedAt); err != nil {
			return nil, err
		}
		rels = append(rels, r)
	}
	return rels, rows.Err()
}

// eventsToBatches splits events into JSON-serializable request bodies of at
// most pushBatchSize each.
func eventsToBatches(events []eventDTO) []map[string]any {
	var out []map[string]any
	for i := 0; i < len(events); i += pushBatchSize {
		end := i + pushBatchSize
		if end > len(events) {
			end = len(events)
		}
		out = append(out, map[string]any{"events": events[i:end]})
	}
	return out
}

func claimsToBatches(claims []claimDTO, evidence []claimEvidenceItem) []map[string]any {
	var out []map[string]any
	if len(claims) == 0 {
		return out
	}
	// Send the evidence list with the first batch so dependent FKs resolve
	// before the rest of the claims arrive. Server upserts evidence after
	// claims, so this works regardless of batch ordering, but consolidating
	// keeps the wire chatter low.
	for i := 0; i < len(claims); i += pushBatchSize {
		end := i + pushBatchSize
		if end > len(claims) {
			end = len(claims)
		}
		body := map[string]any{"claims": claims[i:end]}
		if i == 0 && len(evidence) > 0 {
			body["evidence"] = evidence
		}
		out = append(out, body)
	}
	return out
}

func relsToBatches(rels []relationshipDTO) []map[string]any {
	var out []map[string]any
	for i := 0; i < len(rels); i += pushBatchSize {
		end := i + pushBatchSize
		if end > len(rels) {
			end = len(rels)
		}
		out = append(out, map[string]any{"relationships": rels[i:end]})
	}
	return out
}

// batchToAny is just an explicit shim for type clarity in handlePush — the
// Go compiler accepts the cast implicitly, but spelling it out documents
// that pushBatched takes the same shape regardless of resource type.
func batchToAny(in []map[string]any) []map[string]any {
	return in
}

func pushBatched(ctx context.Context, client *http.Client, endpoint, token, resource string, batches []map[string]any) (int, error) {
	totalAccepted := 0
	for i, body := range batches {
		buf, err := json.Marshal(body)
		if err != nil {
			return totalAccepted, fmt.Errorf("encode %s batch %d: %w", resource, i, err)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(buf))
		if err != nil {
			return totalAccepted, fmt.Errorf("build %s request: %w", resource, err)
		}
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := client.Do(req)
		if err != nil {
			return totalAccepted, fmt.Errorf("post %s batch %d: %w", resource, i, err)
		}
		respBody, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode/100 != 2 {
			return totalAccepted, fmt.Errorf("%s batch %d: server returned %d: %s", resource, i, resp.StatusCode, strings.TrimSpace(string(respBody)))
		}
		var ack appendResponse
		if err := json.Unmarshal(respBody, &ack); err == nil {
			totalAccepted += ack.Accepted
		}
	}
	return totalAccepted, nil
}

func pullEvents(ctx context.Context, client *http.Client, regURL, token string) ([]eventDTO, error) {
	var events []eventDTO
	offset := 0
	for {
		page, err := fetchPage(ctx, client, fmt.Sprintf("%s/v1/events?limit=%d&offset=%d", regURL, pullPageSize, offset), token)
		if err != nil {
			return nil, err
		}
		var body eventsResponse
		if err := json.Unmarshal(page, &body); err != nil {
			return nil, fmt.Errorf("decode events: %w", err)
		}
		events = append(events, body.Events...)
		if len(body.Events) == 0 || offset+len(body.Events) >= body.Total {
			break
		}
		offset += len(body.Events)
	}
	return events, nil
}

func pullClaims(ctx context.Context, client *http.Client, regURL, token string) ([]claimDTO, []claimEvidenceItem, error) {
	var claims []claimDTO
	var evidence []claimEvidenceItem
	offset := 0
	for {
		page, err := fetchPage(ctx, client, fmt.Sprintf("%s/v1/claims?limit=%d&offset=%d", regURL, pullPageSize, offset), token)
		if err != nil {
			return nil, nil, err
		}
		var body claimsResponse
		if err := json.Unmarshal(page, &body); err != nil {
			return nil, nil, fmt.Errorf("decode claims: %w", err)
		}
		claims = append(claims, body.Claims...)
		evidence = append(evidence, body.Evidence...)
		if len(body.Claims) == 0 || offset+len(body.Claims) >= body.Total {
			break
		}
		offset += len(body.Claims)
	}
	return claims, evidence, nil
}

func pullRelationships(ctx context.Context, client *http.Client, regURL, token string) ([]relationshipDTO, error) {
	var rels []relationshipDTO
	offset := 0
	for {
		page, err := fetchPage(ctx, client, fmt.Sprintf("%s/v1/relationships?limit=%d&offset=%d", regURL, pullPageSize, offset), token)
		if err != nil {
			return nil, err
		}
		var body relationshipsResponse
		if err := json.Unmarshal(page, &body); err != nil {
			return nil, fmt.Errorf("decode relationships: %w", err)
		}
		rels = append(rels, body.Relationships...)
		if len(body.Relationships) == 0 || offset+len(body.Relationships) >= body.Total {
			break
		}
		offset += len(body.Relationships)
	}
	return rels, nil
}

func fetchPage(ctx context.Context, client *http.Client, endpoint, token string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("server returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return body, nil
}

// persistPulledEvents inserts events that don't already exist locally.
// Returns the number of newly inserted rows.
func persistPulledEvents(ctx context.Context, db *sql.DB, events []eventDTO) (int, error) {
	inserted := 0
	for _, e := range events {
		metaJSON, _ := json.Marshal(e.Metadata)
		res, err := db.ExecContext(ctx,
			`INSERT INTO events (id, run_id, schema_version, content, source_input_id, timestamp, metadata_json, ingested_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?)
			 ON CONFLICT(id) DO NOTHING`,
			e.ID, e.RunID, e.SchemaVersion, e.Content, e.SourceInputID, e.Timestamp, string(metaJSON), e.IngestedAt,
		)
		if err != nil {
			return inserted, fmt.Errorf("insert event %s: %w", e.ID, err)
		}
		if n, _ := res.RowsAffected(); n > 0 {
			inserted++
		}
	}
	return inserted, nil
}

func persistPulledClaims(ctx context.Context, db *sql.DB, claims []claimDTO, evidence []claimEvidenceItem) (int, error) {
	inserted := 0
	for _, c := range claims {
		res, err := db.ExecContext(ctx,
			`INSERT INTO claims (id, text, type, confidence, status, created_at)
			 VALUES (?, ?, ?, ?, ?, ?)
			 ON CONFLICT(id) DO NOTHING`,
			c.ID, c.Text, c.Type, c.Confidence, c.Status, c.CreatedAt,
		)
		if err != nil {
			return inserted, fmt.Errorf("insert claim %s: %w", c.ID, err)
		}
		if n, _ := res.RowsAffected(); n > 0 {
			inserted++
		}
	}
	for _, link := range evidence {
		if _, err := db.ExecContext(ctx,
			`INSERT INTO claim_evidence (claim_id, event_id) VALUES (?, ?) ON CONFLICT DO NOTHING`,
			link.ClaimID, link.EventID,
		); err != nil {
			return inserted, fmt.Errorf("insert evidence (%s, %s): %w", link.ClaimID, link.EventID, err)
		}
	}
	return inserted, nil
}

// stampPullProvenance mutates each pulled event so its metadata records
// where it came from and when. The query engine surfaces these to the user
// at answer time so claims from a registry are distinguishable from local
// ones — the federation contract is "show me where each fact came from."
//
// Provenance is only added if not already set, so an event that was first
// pulled from registry A and then re-pulled from registry B keeps its
// original origin (consistent with first-write-wins on event id).
func stampPullProvenance(events []eventDTO, regURL string, at time.Time) {
	stamp := at.Format(time.RFC3339)
	for i := range events {
		if events[i].Metadata == nil {
			events[i].Metadata = map[string]string{}
		}
		if _, exists := events[i].Metadata[provenanceRegistryKey]; !exists {
			events[i].Metadata[provenanceRegistryKey] = regURL
			events[i].Metadata[provenancePulledAtKey] = stamp
		}
	}
}

func loadAllEmbeddingsForPush(ctx context.Context, db *sql.DB) ([]embeddingDTO, error) {
	rows, err := db.QueryContext(ctx, `SELECT entity_id, entity_type, vector, model, dimensions FROM embeddings`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []embeddingDTO
	for rows.Next() {
		var (
			e    embeddingDTO
			blob []byte
			dims int64
		)
		if err := rows.Scan(&e.EntityID, &e.EntityType, &blob, &e.Model, &dims); err != nil {
			return nil, err
		}
		vec, err := embedding.DecodeVector(blob)
		if err != nil {
			return nil, fmt.Errorf("decode embedding %s/%s: %w", e.EntityID, e.EntityType, err)
		}
		e.Vector = vec
		e.Dimensions = int(dims)
		out = append(out, e)
	}
	return out, rows.Err()
}

func embeddingsToBatches(records []embeddingDTO) []map[string]any {
	var out []map[string]any
	for i := 0; i < len(records); i += pushBatchSize {
		end := i + pushBatchSize
		if end > len(records) {
			end = len(records)
		}
		out = append(out, map[string]any{"embeddings": records[i:end]})
	}
	return out
}

func pullEmbeddings(ctx context.Context, client *http.Client, regURL, token string) ([]embeddingDTO, error) {
	var out []embeddingDTO
	offset := 0
	for {
		page, err := fetchPage(ctx, client, fmt.Sprintf("%s/v1/embeddings?limit=%d&offset=%d", regURL, pullPageSize, offset), token)
		if err != nil {
			return nil, err
		}
		var body embeddingsResponse
		if err := json.Unmarshal(page, &body); err != nil {
			return nil, fmt.Errorf("decode embeddings: %w", err)
		}
		out = append(out, body.Embeddings...)
		if len(body.Embeddings) == 0 || offset+len(body.Embeddings) >= body.Total {
			break
		}
		offset += len(body.Embeddings)
	}
	return out, nil
}

// persistPulledEmbeddings upserts pulled embeddings via the existing repo,
// which encodes the vector back to a binary BLOB. We don't track "newly
// inserted vs updated" separately — embeddings are derived data, last
// write wins is the right semantic.
func persistPulledEmbeddings(ctx context.Context, db *sql.DB, records []embeddingDTO) (int, error) {
	repo := sqlite.NewEmbeddingRepository(db)
	persisted := 0
	for _, e := range records {
		if err := repo.Upsert(ctx, e.EntityID, e.EntityType, e.Vector, e.Model); err != nil {
			return persisted, fmt.Errorf("upsert embedding %s/%s: %w", e.EntityID, e.EntityType, err)
		}
		persisted++
	}
	return persisted, nil
}

func persistPulledRelationships(ctx context.Context, db *sql.DB, rels []relationshipDTO) (int, error) {
	inserted := 0
	for _, r := range rels {
		res, err := db.ExecContext(ctx,
			`INSERT INTO relationships (id, type, from_claim_id, to_claim_id, created_at)
			 VALUES (?, ?, ?, ?, ?)
			 ON CONFLICT(id) DO NOTHING`,
			r.ID, r.Type, r.FromClaimID, r.ToClaimID, r.CreatedAt,
		)
		if err != nil {
			return inserted, fmt.Errorf("insert relationship %s: %w", r.ID, err)
		}
		if n, _ := res.RowsAffected(); n > 0 {
			inserted++
		}
	}
	return inserted, nil
}
