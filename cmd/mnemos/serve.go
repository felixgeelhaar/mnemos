package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/felixgeelhaar/mnemos/internal/store/sqlite"
)

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
func newServerMux(db *sql.DB) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", handleHealth)
	mux.HandleFunc("/v1/events", makeListEventsHandler(db))
	mux.HandleFunc("/v1/claims", makeListClaimsHandler(db))
	mux.HandleFunc("/v1/relationships", makeListRelationshipsHandler(db))
	mux.HandleFunc("/v1/metrics", makeMetricsHandler(db))
	return logMiddleware(mux)
}

// logMiddleware writes a one-line access log to stderr per request. Keeps
// stdout clean for the boot banner and any future structured output.
func logMiddleware(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		h.ServeHTTP(rw, r)
		fmt.Fprintf(os.Stderr, "%s %s %d %s\n",
			r.Method, r.URL.RequestURI(), rw.status, time.Since(start).Round(time.Microsecond))
	})
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
	Status  string `json:"status"`
	Version string `json:"version"`
}

func handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, healthResponse{Status: "ok", Version: version})
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

func makeListEventsHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
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
}

type claimsResponse struct {
	Claims []claimDTO `json:"claims"`
	Total  int        `json:"total"`
	Limit  int        `json:"limit"`
	Offset int        `json:"offset"`
}

type claimDTO struct {
	ID         string  `json:"id"`
	Text       string  `json:"text"`
	Type       string  `json:"type"`
	Confidence float64 `json:"confidence"`
	Status     string  `json:"status"`
	CreatedAt  string  `json:"created_at"`
}

func makeListClaimsHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
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
		writeJSON(w, http.StatusOK, claimsResponse{Claims: claims, Total: total, Limit: limit, Offset: offset})
	}
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

func makeListRelationshipsHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
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
