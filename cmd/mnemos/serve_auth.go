package main

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/felixgeelhaar/bolt"
	"github.com/felixgeelhaar/mnemos/internal/auth"
	"github.com/felixgeelhaar/mnemos/internal/domain"
)

// actorContextKey tags the resolved user id on a request's context so
// downstream handlers can stamp it into created_by columns. A distinct
// unexported type keeps us from colliding with context keys from other
// packages.
type actorContextKey struct{}

// withActor returns a copy of ctx carrying the given user id.
func withActor(ctx context.Context, userID string) context.Context {
	return context.WithValue(ctx, actorContextKey{}, userID)
}

// actorFromContext returns the user id previously installed via withActor.
// When the request is unauthenticated (reads), falls back to SystemUser so
// the caller always has a non-empty string to stamp.
func actorFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(actorContextKey{}).(string); ok && v != "" {
		return v
	}
	return domain.SystemUser
}

// jwtAuthMiddleware enforces JWT auth on mutating methods. Reads stay
// open: the registry is meant to be browsable, and the blast radius of
// an anonymous GET is bounded by the data we chose to expose.
//
// On POST/PUT/DELETE:
//   - Missing or malformed Authorization header → 401
//   - Invalid signature / expired / revoked token → 401
//   - Valid token → user id from the `sub` claim lands on the request
//     context for created_by stamping.
func jwtAuthMiddleware(verifier *auth.Verifier, h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
			h.ServeHTTP(w, r)
			return
		}

		raw := r.Header.Get("Authorization")
		const prefix = "Bearer "
		if !strings.HasPrefix(raw, prefix) {
			writeError(w, http.StatusUnauthorized, "missing bearer token")
			return
		}
		tokenStr := strings.TrimPrefix(raw, prefix)
		if tokenStr == "" {
			writeError(w, http.StatusUnauthorized, "missing bearer token")
			return
		}

		claims, err := verifier.ParseAndValidate(r.Context(), tokenStr)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "invalid or revoked token")
			return
		}

		h.ServeHTTP(w, r.WithContext(withActor(r.Context(), claims.UserID)))
	})
}

// boltAccessLog returns a middleware that emits one structured access
// log per request. Uses bolt so field names match the rest of the
// codebase; `user_id` is included when authentication resolved an actor
// so we can trace writes back to the issuing identity.
func boltAccessLog(logger *bolt.Logger, h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		h.ServeHTTP(rw, r)

		actor := actorFromContext(r.Context())
		logger.Info().
			Str("method", r.Method).
			Str("path", r.URL.RequestURI()).
			Int("status", rw.status).
			Dur("duration", time.Since(start)).
			Str("user_id", actor).
			Msg("http_request")
	})
}
