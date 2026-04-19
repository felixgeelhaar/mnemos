// Package auth implements the Mnemos identity primitives: JWT issuance,
// validation, and revocation. Tokens are HS256 with a server-side secret;
// claims carry the user id, optional kind (user|agent), and the standard
// jti/iat/exp triplet.
//
// Revocation is opt-in via a denylist (see ports.RevokedTokenRepository)
// — auth middleware looks up the jti on every request. The cost is one
// indexed SQLite read per call; in exchange we get instant revocation
// without rotating the signing secret.
package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/felixgeelhaar/mnemos/internal/domain"
	"github.com/felixgeelhaar/mnemos/internal/ports"
)

// TokenKind distinguishes a human-issued token from an agent-issued one.
// The middleware path is the same; the kind is recorded for audit and
// future per-kind policy (rate limits, scope restrictions).
type TokenKind string

// Supported TokenKind values.
const (
	TokenKindUser  TokenKind = "user"
	TokenKindAgent TokenKind = "agent"
)

// Claims is the parsed shape of a Mnemos JWT.
type Claims struct {
	UserID string    `json:"sub"`
	Kind   TokenKind `json:"knd"`
	jwt.RegisteredClaims
}

// Issuer mints new JWTs. It does not store anything — the resulting
// token string is the only place the plaintext exists. Callers should
// hand it back to the user once and never persist it server-side.
type Issuer struct {
	secret []byte
}

// Verifier parses + validates JWTs and consults the revocation denylist.
type Verifier struct {
	secret  []byte
	revoked ports.RevokedTokenRepository
}

// NewIssuer returns an Issuer signing tokens with the given secret. The
// secret should be at least 32 bytes of random data; see GenerateSecret.
func NewIssuer(secret []byte) *Issuer {
	return &Issuer{secret: secret}
}

// NewVerifier returns a Verifier that validates tokens against the
// given secret and consults revoked for instant-revocation lookups.
func NewVerifier(secret []byte, revoked ports.RevokedTokenRepository) *Verifier {
	return &Verifier{secret: secret, revoked: revoked}
}

// IssueUserToken mints a JWT for a human user, valid for ttl. Returns
// the signed token string and the JTI (so callers that want to record
// the issuance can do so).
func (i *Issuer) IssueUserToken(user domain.User, ttl time.Duration) (token, jti string, err error) {
	return i.issue(user.ID, TokenKindUser, ttl)
}

// IssueAgentToken mints a JWT for an automated agent, valid for ttl.
// Same shape as a user token; the Kind field distinguishes them.
func (i *Issuer) IssueAgentToken(agentID string, ttl time.Duration) (token, jti string, err error) {
	return i.issue(agentID, TokenKindAgent, ttl)
}

func (i *Issuer) issue(subject string, kind TokenKind, ttl time.Duration) (string, string, error) {
	if subject == "" {
		return "", "", errors.New("subject is required")
	}
	if ttl <= 0 {
		return "", "", errors.New("ttl must be positive")
	}
	jti, err := newJTI()
	if err != nil {
		return "", "", fmt.Errorf("generate jti: %w", err)
	}
	now := time.Now().UTC()
	claims := Claims{
		UserID: subject,
		Kind:   kind,
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        jti,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
			Issuer:    "mnemos",
		},
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := tok.SignedString(i.secret)
	if err != nil {
		return "", "", fmt.Errorf("sign token: %w", err)
	}
	return signed, jti, nil
}

// ParseAndValidate parses tokenString, verifies its signature against
// the configured secret, checks expiry, and consults the revocation
// denylist. Returns the validated claims or an error explaining why
// the token was rejected.
func (v *Verifier) ParseAndValidate(ctx context.Context, tokenString string) (*Claims, error) {
	parsed, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(t *jwt.Token) (any, error) {
		// Lock the algorithm to prevent the classic alg=none / alg=RS256
		// confusion attack where an attacker swaps in a different
		// algorithm.
		if t.Method.Alg() != jwt.SigningMethodHS256.Alg() {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return v.secret, nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}))
	if err != nil {
		return nil, fmt.Errorf("parse token: %w", err)
	}

	claims, ok := parsed.Claims.(*Claims)
	if !ok || !parsed.Valid {
		return nil, errors.New("invalid token claims")
	}
	if claims.UserID == "" {
		return nil, errors.New("token missing subject")
	}

	revoked, err := v.revoked.IsRevoked(ctx, claims.ID)
	if err != nil {
		return nil, fmt.Errorf("check revocation: %w", err)
	}
	if revoked {
		return nil, errors.New("token revoked")
	}

	return claims, nil
}

// GenerateSecret returns 32 bytes of cryptographically random data,
// suitable for use as the JWT signing secret. Persist this securely;
// rotating it invalidates every issued token.
func GenerateSecret() ([]byte, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return nil, fmt.Errorf("generate secret: %w", err)
	}
	return b, nil
}

func newJTI() (string, error) {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
