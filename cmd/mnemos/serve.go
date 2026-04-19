package main

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/felixgeelhaar/bolt"
	"github.com/felixgeelhaar/mnemos/internal/auth"
	"github.com/felixgeelhaar/mnemos/internal/domain"
	"github.com/felixgeelhaar/mnemos/internal/embedding"
	"github.com/felixgeelhaar/mnemos/internal/store/sqlite"
)

//go:embed web/index.html
var webIndexHTML []byte

const (
	defaultServePort   = 7777
	maxServePageLimit  = 200
	defaultServeLimit  = 50
	serveReadTimeout   = 10 * time.Second
	serveWriteTimeout  = 30 * time.Second
	serveIdleTimeout   = 60 * time.Second
	serveShutdownGrace = 10 * time.Second
)

// handleServe runs the HTTP registry server. Phase 2B v1 — read-only
// endpoints over the local DB. Push, pull, namespacing, and auth ship in
// follow-up commits, since the read surface is what every later concern
// needs first anyway.
func handleServe(args []string, _ Flags) {
	port := defaultServePort
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--port":
			if i+1 >= len(args) {
				exitWithMnemosError(false, NewUserError("--port requires a value"))
				return
			}
			p, err := strconv.Atoi(args[i+1])
			if err != nil || p < 1 || p > 65535 {
				exitWithMnemosError(false, NewUserError("--port must be a number between 1 and 65535"))
				return
			}
			port = p
			i++
		default:
			exitWithMnemosError(false, NewUserError("unknown serve flag %q", args[i]))
			return
		}
	}
	if envPort := os.Getenv("MNEMOS_SERVE_PORT"); envPort != "" && port == defaultServePort {
		if p, err := strconv.Atoi(envPort); err == nil && p >= 1 && p <= 65535 {
			port = p
		}
	}

	dbPath := resolveDBPath()
	db, err := sqlite.Open(dbPath)
	if err != nil {
		exitWithMnemosError(false, NewSystemError(err, "failed to open database at %q", dbPath))
		return
	}
	defer closeDB(db)

	srv := &http.Server{
		Addr:         fmt.Sprintf(":%d", port),
		Handler:      newServerMux(db),
		ReadTimeout:  serveReadTimeout,
		WriteTimeout: serveWriteTimeout,
		IdleTimeout:  serveIdleTimeout,
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	errCh := make(chan error, 1)
	go func() {
		fmt.Printf("mnemos registry serving on http://localhost:%d (db=%s)\n", port, dbPath)
		fmt.Println("Press Ctrl+C to stop.")
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		exitWithMnemosError(false, NewSystemError(err, "server error"))
	case sig := <-stop:
		fmt.Fprintf(os.Stderr, "\nreceived %s, shutting down...\n", sig)
		ctx, cancel := context.WithTimeout(context.Background(), serveShutdownGrace)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "shutdown: %v\n", err)
		}
	}
}

// newServerMux wires the routes. Exported in package for httptest in
// serve_test.go without booting a real listener.
//
// Auth model: reads are open; mutating methods require a valid Mnemos
// JWT signed with the server secret. The secret is resolved from
// MNEMOS_JWT_SECRET or a per-install file (auto-created on first boot).
// Revoked JTIs are honored via the RevokedTokenRepository denylist.
func newServerMux(db *sql.DB) http.Handler {
	_, projectRoot, _ := findProjectDB()
	secretPath := auth.DefaultSecretPath(projectRoot)
	secret, created, err := auth.LoadOrCreateSecret(secretPath)
	if err != nil {
		// Secret resolution failing at boot is fatal — without it the
		// server can't verify any token. Fail loudly so the operator can
		// fix it (e.g. set MNEMOS_JWT_SECRET).
		fmt.Fprintf(os.Stderr, "serve: load JWT secret: %v\n", err)
		os.Exit(int(ExitError))
	}
	if created {
		fmt.Fprintf(os.Stderr, "serve: generated new JWT secret at %s — previously-issued tokens are invalid\n", secretPath)
	}
	verifier := auth.NewVerifier(secret, sqlite.NewRevokedTokenRepository(db))

	mux := http.NewServeMux()
	mux.HandleFunc("/", handleWebRoot)
	mux.HandleFunc("/health", makeHealthHandler(db))
	mux.HandleFunc("/v1/events", makeEventsHandler(db))
	mux.HandleFunc("/v1/claims", makeClaimsHandler(db))
	mux.HandleFunc("/v1/relationships", makeRelationshipsHandler(db))
	mux.HandleFunc("/v1/embeddings", makeEmbeddingsHandler(db))
	mux.HandleFunc("/v1/metrics", makeMetricsHandler(db))

	logger := bolt.New(bolt.NewJSONHandler(os.Stderr))
	return boltAccessLog(logger, jwtAuthMiddleware(verifier, mux))
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

type healthResponse struct {
	Status  string             `json:"status"`
	Version string             `json:"version"`
	Healthy *bool              `json:"healthy,omitempty"`
	Checks  []healthCheck      `json:"checks,omitempty"`
}

// makeHealthHandler returns the /health handler. Default response is
// the cheap shallow check (status + version) for liveness probes;
// callers asking for ?deep=true get the full subsystem report so an
// orchestrator can readiness-gate on it.
//
// Returns 503 when deep=true reveals a failed probe so HTTP-aware
// load balancers / Kubernetes readiness gates can react without
// parsing the JSON.
func makeHealthHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("deep") != "true" {
			writeJSON(w, http.StatusOK, healthResponse{Status: "ok", Version: version})
			return
		}
		result := runHealthChecks(r.Context(), db)
		status := http.StatusOK
		if !result.Healthy {
			status = http.StatusServiceUnavailable
		}
		writeJSON(w, status, healthResponse{
			Status:  ternary(result.Healthy, "ok", "degraded"),
			Version: version,
			Healthy: &result.Healthy,
			Checks:  result.Checks,
		})
	}
}

func ternary(cond bool, t, f string) string {
	if cond {
		return t
	}
	return f
}

// handleWebRoot serves the embedded single-page UI at GET /. Any other
// path returns 404 — we don't want catch-all behavior masking real route
// typos like /v1/clams. Unsupported methods get 405.
func handleWebRoot(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}
	if _, err := w.Write(webIndexHTML); err != nil {
		fmt.Fprintf(os.Stderr, "serve web: %v\n", err)
	}
}

type eventsResponse struct {
	Events []eventDTO `json:"events"`
	Total  int        `json:"total"`
	Limit  int        `json:"limit"`
	Offset int        `json:"offset"`
}

type eventDTO struct {
	ID            string            `json:"id"`
	RunID         string            `json:"run_id"`
	SchemaVersion string            `json:"schema_version"`
	Content       string            `json:"content"`
	SourceInputID string            `json:"source_input_id"`
	Timestamp     string            `json:"timestamp"`
	Metadata      map[string]string `json:"metadata"`
	IngestedAt    string            `json:"ingested_at"`
}

func makeEventsHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			listEventsHandler(db, w, r)
		case http.MethodPost:
			appendEventsHandler(db, w, r)
		default:
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	}
}

func listEventsHandler(db *sql.DB, w http.ResponseWriter, r *http.Request) {
	limit, offset := parsePaginationFromQuery(r)
	ctx := r.Context()

	var total int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM events`).Scan(&total); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("count events: %v", err))
		return
	}

	rows, err := db.QueryContext(ctx,
		`SELECT id, run_id, schema_version, content, source_input_id, timestamp, metadata_json, ingested_at
		 FROM events ORDER BY timestamp DESC LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("list events: %v", err))
		return
	}
	defer func() { _ = rows.Close() }()

	var events []eventDTO
	for rows.Next() {
		var (
			e        eventDTO
			metaJSON string
		)
		if err := rows.Scan(&e.ID, &e.RunID, &e.SchemaVersion, &e.Content, &e.SourceInputID, &e.Timestamp, &metaJSON, &e.IngestedAt); err != nil {
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("scan event: %v", err))
			return
		}
		e.Metadata = map[string]string{}
		_ = json.Unmarshal([]byte(metaJSON), &e.Metadata)
		events = append(events, e)
	}
	writeJSON(w, http.StatusOK, eventsResponse{Events: events, Total: total, Limit: limit, Offset: offset})
}

type claimsResponse struct {
	Claims   []claimDTO          `json:"claims"`
	Evidence []claimEvidenceItem `json:"evidence,omitempty"`
	Total    int                 `json:"total"`
	Limit    int                 `json:"limit"`
	Offset   int                 `json:"offset"`
}

type claimDTO struct {
	ID         string  `json:"id"`
	Text       string  `json:"text"`
	Type       string  `json:"type"`
	Confidence float64 `json:"confidence"`
	Status     string  `json:"status"`
	CreatedAt  string  `json:"created_at"`
}

func makeClaimsHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			listClaimsHandler(db, w, r)
		case http.MethodPost:
			appendClaimsHandler(db, w, r)
		default:
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	}
}

func listClaimsHandler(db *sql.DB, w http.ResponseWriter, r *http.Request) {
	limit, offset := parsePaginationFromQuery(r)
	typeFilter := r.URL.Query().Get("type")
	statusFilter := r.URL.Query().Get("status")
	if typeFilter != "" && !validClaimType(typeFilter) {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid type %q", typeFilter))
		return
	}
	if statusFilter != "" && !validClaimStatus(statusFilter) {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid status %q", statusFilter))
		return
	}
	ctx := r.Context()

	var (
		where string
		args  []any
	)
	if typeFilter != "" && statusFilter != "" {
		where = " WHERE type = ? AND status = ?"
		args = []any{typeFilter, statusFilter}
	} else if typeFilter != "" {
		where = " WHERE type = ?"
		args = []any{typeFilter}
	} else if statusFilter != "" {
		where = " WHERE status = ?"
		args = []any{statusFilter}
	}

	var total int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM claims"+where, args...).Scan(&total); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("count claims: %v", err))
		return
	}

	rowArgs := append(append([]any{}, args...), limit, offset)
	//nolint:gosec // G202: where clause is built from validated constant fragments only; values pass through ? placeholders
	rows, err := db.QueryContext(ctx,
		"SELECT id, text, type, confidence, status, created_at FROM claims"+where+" ORDER BY created_at DESC LIMIT ? OFFSET ?",
		rowArgs...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("list claims: %v", err))
		return
	}
	defer func() { _ = rows.Close() }()

	var claims []claimDTO
	for rows.Next() {
		var c claimDTO
		if err := rows.Scan(&c.ID, &c.Text, &c.Type, &c.Confidence, &c.Status, &c.CreatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("scan claim: %v", err))
			return
		}
		claims = append(claims, c)
	}

	evidence, evErr := loadEvidenceForClaims(ctx, db, claims)
	if evErr != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("load evidence: %v", evErr))
		return
	}

	writeJSON(w, http.StatusOK, claimsResponse{Claims: claims, Evidence: evidence, Total: total, Limit: limit, Offset: offset})
}

// loadEvidenceForClaims returns the (claim_id, event_id) link rows for the
// supplied claim IDs. Empty input → empty output. Used by GET /v1/claims so
// pull can recover the evidence relations alongside the claims themselves.
func loadEvidenceForClaims(ctx context.Context, db *sql.DB, claims []claimDTO) ([]claimEvidenceItem, error) {
	if len(claims) == 0 {
		return nil, nil
	}
	placeholders := make([]string, 0, len(claims))
	args := make([]any, 0, len(claims))
	for _, c := range claims {
		placeholders = append(placeholders, "?")
		args = append(args, c.ID)
	}
	//nolint:gosec // G202: placeholders are literal "?", IDs flow through ? bindings
	q := "SELECT claim_id, event_id FROM claim_evidence WHERE claim_id IN (" + strings.Join(placeholders, ",") + ")"
	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []claimEvidenceItem
	for rows.Next() {
		var item claimEvidenceItem
		if err := rows.Scan(&item.ClaimID, &item.EventID); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

type relationshipsResponse struct {
	Relationships []relationshipDTO `json:"relationships"`
	Total         int               `json:"total"`
	Limit         int               `json:"limit"`
	Offset        int               `json:"offset"`
}

type relationshipDTO struct {
	ID          string `json:"id"`
	Type        string `json:"type"`
	FromClaimID string `json:"from_claim_id"`
	ToClaimID   string `json:"to_claim_id"`
	CreatedAt   string `json:"created_at"`
}

func makeRelationshipsHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			listRelationshipsHandler(db, w, r)
		case http.MethodPost:
			appendRelationshipsHandler(db, w, r)
		default:
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	}
}

func listRelationshipsHandler(db *sql.DB, w http.ResponseWriter, r *http.Request) {
	limit, offset := parsePaginationFromQuery(r)
	typeFilter := r.URL.Query().Get("type")
	ctx := r.Context()

	var (
		where string
		args  []any
	)
	if typeFilter != "" {
		if typeFilter != "supports" && typeFilter != "contradicts" {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid type %q (want supports or contradicts)", typeFilter))
			return
		}
		where = " WHERE type = ?"
		args = []any{typeFilter}
	}

	var total int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM relationships"+where, args...).Scan(&total); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("count relationships: %v", err))
		return
	}

	rowArgs := append(append([]any{}, args...), limit, offset)
	//nolint:gosec // G202: where clause is built from validated constant fragments only; values pass through ? placeholders
	rows, err := db.QueryContext(ctx,
		"SELECT id, type, from_claim_id, to_claim_id, created_at FROM relationships"+where+" ORDER BY created_at DESC LIMIT ? OFFSET ?",
		rowArgs...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("list relationships: %v", err))
		return
	}
	defer func() { _ = rows.Close() }()

	var rels []relationshipDTO
	for rows.Next() {
		var rel relationshipDTO
		if err := rows.Scan(&rel.ID, &rel.Type, &rel.FromClaimID, &rel.ToClaimID, &rel.CreatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("scan relationship: %v", err))
			return
		}
		rels = append(rels, rel)
	}
	writeJSON(w, http.StatusOK, relationshipsResponse{Relationships: rels, Total: total, Limit: limit, Offset: offset})
}

// appendEventsRequest is the body for POST /v1/events. Single-event submits
// are common (raw streams) but a batch shape future-proofs the endpoint and
// keeps DTOs symmetric with claims/relationships.
type appendEventsRequest struct {
	Events []eventDTO `json:"events"`
}

type appendResponse struct {
	Accepted int `json:"accepted"`
	Skipped  int `json:"skipped"`
}

const maxRequestBytes = 5 * 1024 * 1024 // 5 MB; bigger payloads should chunk

func decodeJSON(r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(nil, r.Body, maxRequestBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		if errors.Is(err, io.EOF) {
			return errors.New("request body is empty")
		}
		return err
	}
	if dec.More() {
		return errors.New("request body has trailing content after the JSON object")
	}
	return nil
}

func parseTimeFlexible(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unrecognized time format %q (want RFC3339)", s)
}

func appendEventsHandler(db *sql.DB, w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, domain.ScopeEventsWrite) {
		return
	}
	var req appendEventsRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("decode body: %v", err))
		return
	}
	if len(req.Events) == 0 {
		writeError(w, http.StatusBadRequest, "events array is empty")
		return
	}

	// F.4: enforce the bearer's run whitelist before any DB write so
	// a partial batch can't sneak through. We pre-check every run_id
	// up front; an empty whitelist short-circuits the loop.
	if allowed := allowedRunsFromContext(r.Context()); len(allowed) > 0 {
		allowedSet := make(map[string]struct{}, len(allowed))
		for _, a := range allowed {
			allowedSet[a] = struct{}{}
		}
		for i, e := range req.Events {
			if _, ok := allowedSet[e.RunID]; !ok {
				writeError(w, http.StatusForbidden, fmt.Sprintf("events[%d].run_id %q not in token whitelist", i, e.RunID))
				return
			}
		}
	}

	repo := sqlite.NewEventRepository(db)
	ctx := r.Context()
	actor := actorFromContext(ctx)
	now := time.Now().UTC()
	accepted := 0
	for i, e := range req.Events {
		ts, err := parseTimeFlexible(e.Timestamp)
		if err != nil {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("events[%d].timestamp: %v", i, err))
			return
		}
		ingested, err := parseTimeFlexible(e.IngestedAt)
		if err != nil {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("events[%d].ingested_at: %v", i, err))
			return
		}
		if ingested.IsZero() {
			ingested = now
		}
		event := domain.Event{
			ID:            e.ID,
			RunID:         e.RunID,
			SchemaVersion: e.SchemaVersion,
			Content:       e.Content,
			SourceInputID: e.SourceInputID,
			Timestamp:     ts,
			Metadata:      e.Metadata,
			IngestedAt:    ingested,
			CreatedBy:     actor,
		}
		if err := repo.Append(ctx, event); err != nil {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("events[%d]: %v", i, err))
			return
		}
		accepted++
	}
	writeJSON(w, http.StatusCreated, appendResponse{Accepted: accepted})
}

type appendClaimsRequest struct {
	Claims   []claimDTO          `json:"claims"`
	Evidence []claimEvidenceItem `json:"evidence,omitempty"`
}

type claimEvidenceItem struct {
	ClaimID string `json:"claim_id"`
	EventID string `json:"event_id"`
}

func appendClaimsHandler(db *sql.DB, w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, domain.ScopeClaimsWrite) {
		return
	}
	var req appendClaimsRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("decode body: %v", err))
		return
	}
	if len(req.Claims) == 0 {
		writeError(w, http.StatusBadRequest, "claims array is empty")
		return
	}

	claims := make([]domain.Claim, 0, len(req.Claims))
	now := time.Now().UTC()
	actor := actorFromContext(r.Context())
	for i, c := range req.Claims {
		if c.Type != "" && !validClaimType(c.Type) {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("claims[%d].type %q invalid", i, c.Type))
			return
		}
		if c.Status != "" && !validClaimStatus(c.Status) {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("claims[%d].status %q invalid", i, c.Status))
			return
		}
		created, err := parseTimeFlexible(c.CreatedAt)
		if err != nil {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("claims[%d].created_at: %v", i, err))
			return
		}
		if created.IsZero() {
			created = now
		}
		claim := domain.Claim{
			ID:         c.ID,
			Text:       c.Text,
			Type:       domain.ClaimType(c.Type),
			Confidence: c.Confidence,
			Status:     domain.ClaimStatus(c.Status),
			CreatedAt:  created,
			CreatedBy:  actor,
		}
		claims = append(claims, claim)
	}

	repo := sqlite.NewClaimRepository(db)
	ctx := r.Context()

	// F.4.b: if the bearer is run-scoped, every event referenced by
	// the request's evidence links must belong to an allowed run.
	// We pre-check before any DB write so failures don't leave
	// orphan claims behind.
	if allowed := allowedRunsFromContext(ctx); len(allowed) > 0 && len(req.Evidence) > 0 {
		eventIDs := make([]string, 0, len(req.Evidence))
		for _, e := range req.Evidence {
			eventIDs = append(eventIDs, e.EventID)
		}
		bad, badRun, err := checkEventRunsAllowed(ctx, db, eventIDs, allowed)
		if err != nil {
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("run-scope check: %v", err))
			return
		}
		if bad != "" {
			writeError(w, http.StatusForbidden, fmt.Sprintf("evidence event %q (run %q) not in token whitelist", bad, badRun))
			return
		}
	}

	if err := repo.Upsert(ctx, claims); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("upsert claims: %v", err))
		return
	}

	if len(req.Evidence) > 0 {
		links := make([]domain.ClaimEvidence, 0, len(req.Evidence))
		for _, e := range req.Evidence {
			links = append(links, domain.ClaimEvidence{ClaimID: e.ClaimID, EventID: e.EventID})
		}
		if err := repo.UpsertEvidence(ctx, links); err != nil {
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("upsert evidence: %v", err))
			return
		}
	}
	writeJSON(w, http.StatusCreated, appendResponse{Accepted: len(claims)})
}

type appendRelationshipsRequest struct {
	Relationships []relationshipDTO `json:"relationships"`
}

func appendRelationshipsHandler(db *sql.DB, w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, domain.ScopeRelationshipsWrite) {
		return
	}
	var req appendRelationshipsRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("decode body: %v", err))
		return
	}
	if len(req.Relationships) == 0 {
		writeError(w, http.StatusBadRequest, "relationships array is empty")
		return
	}

	rels := make([]domain.Relationship, 0, len(req.Relationships))
	now := time.Now().UTC()
	actor := actorFromContext(r.Context())
	for i, rel := range req.Relationships {
		if rel.Type != "supports" && rel.Type != "contradicts" {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("relationships[%d].type %q invalid (want supports or contradicts)", i, rel.Type))
			return
		}
		created, err := parseTimeFlexible(rel.CreatedAt)
		if err != nil {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("relationships[%d].created_at: %v", i, err))
			return
		}
		if created.IsZero() {
			created = now
		}
		rels = append(rels, domain.Relationship{
			ID:          rel.ID,
			Type:        domain.RelationshipType(rel.Type),
			FromClaimID: rel.FromClaimID,
			ToClaimID:   rel.ToClaimID,
			CreatedAt:   created,
			CreatedBy:   actor,
		})
	}

	// F.4.b: relationships span claims; both endpoint claims' evidence
	// events must lie in the bearer's allowed runs.
	if allowed := allowedRunsFromContext(r.Context()); len(allowed) > 0 {
		claimIDs := make([]string, 0, len(rels)*2)
		seen := map[string]struct{}{}
		for _, rel := range rels {
			for _, id := range []string{rel.FromClaimID, rel.ToClaimID} {
				if _, dup := seen[id]; dup {
					continue
				}
				seen[id] = struct{}{}
				claimIDs = append(claimIDs, id)
			}
		}
		evIDs, err := claimEventIDs(r.Context(), db, claimIDs)
		if err != nil {
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("run-scope lookup: %v", err))
			return
		}
		bad, badRun, err := checkEventRunsAllowed(r.Context(), db, evIDs, allowed)
		if err != nil {
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("run-scope check: %v", err))
			return
		}
		if bad != "" {
			writeError(w, http.StatusForbidden, fmt.Sprintf("relationship references event %q (run %q) not in token whitelist", bad, badRun))
			return
		}
	}

	repo := sqlite.NewRelationshipRepository(db)
	if err := repo.Upsert(r.Context(), rels); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("upsert relationships: %v", err))
		return
	}
	writeJSON(w, http.StatusCreated, appendResponse{Accepted: len(rels)})
}

// embeddingDTO carries a vector as a JSON array of float32. Larger on the
// wire than a binary blob (typically 5–8× the raw byte size for 768-dim
// vectors), but debuggable, language-agnostic, and bit-exact through the
// encode/decode cycle since float32 has well-defined JSON behavior.
type embeddingDTO struct {
	EntityID   string    `json:"entity_id"`
	EntityType string    `json:"entity_type"`
	Vector     []float32 `json:"vector"`
	Model      string    `json:"model"`
	Dimensions int       `json:"dimensions"`
}

type embeddingsResponse struct {
	Embeddings []embeddingDTO `json:"embeddings"`
	Total      int            `json:"total"`
	Limit      int            `json:"limit"`
	Offset     int            `json:"offset"`
}

type appendEmbeddingsRequest struct {
	Embeddings []embeddingDTO `json:"embeddings"`
}

func makeEmbeddingsHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			listEmbeddingsHandler(db, w, r)
		case http.MethodPost:
			appendEmbeddingsHandler(db, w, r)
		default:
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	}
}

func listEmbeddingsHandler(db *sql.DB, w http.ResponseWriter, r *http.Request) {
	limit, offset := parsePaginationFromQuery(r)
	typeFilter := r.URL.Query().Get("entity_type")
	ctx := r.Context()

	var (
		where string
		args  []any
	)
	if typeFilter != "" {
		if typeFilter != "event" && typeFilter != "claim" {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid entity_type %q (want event or claim)", typeFilter))
			return
		}
		where = " WHERE entity_type = ?"
		args = []any{typeFilter}
	}

	var total int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM embeddings"+where, args...).Scan(&total); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("count embeddings: %v", err))
		return
	}

	rowArgs := append(append([]any{}, args...), limit, offset)
	//nolint:gosec // G202: where clause is built from validated constant fragments only; values pass through ? placeholders
	rows, err := db.QueryContext(ctx,
		"SELECT entity_id, entity_type, vector, model, dimensions FROM embeddings"+where+" ORDER BY entity_type, entity_id LIMIT ? OFFSET ?",
		rowArgs...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("list embeddings: %v", err))
		return
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
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("scan embedding: %v", err))
			return
		}
		vec, err := embedding.DecodeVector(blob)
		if err != nil {
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("decode embedding for %s/%s: %v", e.EntityID, e.EntityType, err))
			return
		}
		e.Vector = vec
		e.Dimensions = int(dims)
		out = append(out, e)
	}
	writeJSON(w, http.StatusOK, embeddingsResponse{Embeddings: out, Total: total, Limit: limit, Offset: offset})
}

func appendEmbeddingsHandler(db *sql.DB, w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, domain.ScopeEmbeddingsWrite) {
		return
	}
	var req appendEmbeddingsRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("decode body: %v", err))
		return
	}
	if len(req.Embeddings) == 0 {
		writeError(w, http.StatusBadRequest, "embeddings array is empty")
		return
	}

	repo := sqlite.NewEmbeddingRepository(db)
	ctx := r.Context()
	actor := actorFromContext(ctx)

	// F.4.b: validate every embedding's entity belongs to an
	// allowed run before writing any. Event entities are checked
	// directly; claim entities derive their runs through evidence.
	if allowed := allowedRunsFromContext(ctx); len(allowed) > 0 {
		var eventIDs, claimIDs []string
		for _, e := range req.Embeddings {
			switch e.EntityType {
			case "event":
				eventIDs = append(eventIDs, e.EntityID)
			case "claim":
				claimIDs = append(claimIDs, e.EntityID)
			}
		}
		extraEvents, err := claimEventIDs(ctx, db, claimIDs)
		if err != nil {
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("run-scope lookup: %v", err))
			return
		}
		eventIDs = append(eventIDs, extraEvents...)
		bad, badRun, err := checkEventRunsAllowed(ctx, db, eventIDs, allowed)
		if err != nil {
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("run-scope check: %v", err))
			return
		}
		if bad != "" {
			writeError(w, http.StatusForbidden, fmt.Sprintf("embedding entity references event %q (run %q) not in token whitelist", bad, badRun))
			return
		}
	}

	accepted := 0
	for i, e := range req.Embeddings {
		if e.EntityID == "" {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("embeddings[%d].entity_id is required", i))
			return
		}
		if e.EntityType != "event" && e.EntityType != "claim" {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("embeddings[%d].entity_type %q invalid (want event or claim)", i, e.EntityType))
			return
		}
		if len(e.Vector) == 0 {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("embeddings[%d].vector is empty", i))
			return
		}
		if e.Dimensions != 0 && e.Dimensions != len(e.Vector) {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("embeddings[%d]: dimensions=%d but vector length=%d", i, e.Dimensions, len(e.Vector)))
			return
		}
		if err := repo.UpsertAs(ctx, e.EntityID, e.EntityType, e.Vector, e.Model, actor); err != nil {
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("upsert embedding %s/%s: %v", e.EntityID, e.EntityType, err))
			return
		}
		accepted++
	}
	writeJSON(w, http.StatusCreated, appendResponse{Accepted: accepted})
}

type metricsResponse struct {
	Runs            int64 `json:"runs"`
	Events          int64 `json:"events"`
	Claims          int64 `json:"claims"`
	ContestedClaims int64 `json:"contested_claims"`
	Relationships   int64 `json:"relationships"`
	Contradictions  int64 `json:"contradictions"`
	Embeddings      int64 `json:"embeddings"`
}

func makeMetricsHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		writeJSON(w, http.StatusOK, metricsResponse{
			Runs:            countRowsServe(db, `SELECT COUNT(DISTINCT run_id) FROM events WHERE run_id <> ''`),
			Events:          countRowsServe(db, `SELECT COUNT(*) FROM events`),
			Claims:          countRowsServe(db, `SELECT COUNT(*) FROM claims`),
			ContestedClaims: countRowsServe(db, `SELECT COUNT(*) FROM claims WHERE status = 'contested'`),
			Relationships:   countRowsServe(db, `SELECT COUNT(*) FROM relationships`),
			Contradictions:  countRowsServe(db, `SELECT COUNT(*) FROM relationships WHERE type = 'contradicts'`),
			Embeddings:      countRowsServe(db, `SELECT COUNT(*) FROM embeddings`),
		})
	}
}

func countRowsServe(db *sql.DB, q string) int64 {
	var n int64
	if err := db.QueryRow(q).Scan(&n); err != nil {
		return 0
	}
	return n
}

// parsePaginationFromQuery reads ?limit and ?offset query params with the
// same defaults/caps as the MCP browse handlers. Invalid values are
// silently coerced rather than rejected — query strings are best-effort.
func parsePaginationFromQuery(r *http.Request) (int, int) {
	limit := defaultServeLimit
	offset := 0
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	if limit > maxServePageLimit {
		limit = maxServePageLimit
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}
	return limit, offset
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		// Body already partially written — log to stderr but can't change the status.
		fmt.Fprintf(os.Stderr, "writeJSON: %v\n", err)
	}
}

type errorResponse struct {
	Error string `json:"error"`
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, errorResponse{Error: msg})
}
