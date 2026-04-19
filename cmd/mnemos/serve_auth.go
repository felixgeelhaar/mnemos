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

// scopesContextKey tags the bearer's scope list onto the request
// context so handlers can call requireScope without re-parsing the
// token.
type scopesContextKey struct{}

// withActor returns a copy of ctx carrying the given user id.
func withActor(ctx context.Context, userID string) context.Context {
	return context.WithValue(ctx, actorContextKey{}, userID)
}

// withScopes returns a copy of ctx carrying the bearer's scope list.
func withScopes(ctx context.Context, scopes []string) context.Context {
	return context.WithValue(ctx, scopesContextKey{}, scopes)
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

// scopesFromContext returns the bearer's scope list, or an empty slice
// when no token was presented (read-only requests).
func scopesFromContext(ctx context.Context) []string {
	if v, ok := ctx.Value(scopesContextKey{}).([]string); ok {
		return v
	}
	return nil
}

// requireScope returns true and writes nothing when the request's
// scope list grants want; otherwise it writes a 403 and returns false.
// Handlers should `if !requireScope(w, r, "events:write") { return }`
// at the very top of their POST path.
func requireScope(w http.ResponseWriter, r *http.Request, want string) bool {
	for _, s := range scopesFromContext(r.Context()) {
		if s == domain.ScopeWildcard || s == want {
			return true
		}
	}
	writeError(w, http.StatusForbidden, "missing required scope: "+want)
	return false
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

		ctx := withActor(r.Context(), claims.UserID)
		ctx = withScopes(ctx, claims.Scopes)
		h.ServeHTTP(w, r.WithContext(ctx))
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
