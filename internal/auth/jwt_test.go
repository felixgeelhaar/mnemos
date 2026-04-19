package auth

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/felixgeelhaar/mnemos/internal/domain"
)

// fakeRevokedRepo is a minimal in-memory implementation for tests.
type fakeRevokedRepo struct {
	revoked map[string]bool
}

func newFakeRevoked() *fakeRevokedRepo {
	return &fakeRevokedRepo{revoked: map[string]bool{}}
}

func (f *fakeRevokedRepo) Add(_ context.Context, t domain.RevokedToken) error {
	f.revoked[t.JTI] = true
	return nil
}
func (f *fakeRevokedRepo) IsRevoked(_ context.Context, jti string) (bool, error) {
	return f.revoked[jti], nil
}
func (f *fakeRevokedRepo) PurgeExpired(_ context.Context, _ time.Time) (int, error) {
	return 0, nil
}

func TestIssueAndValidate_RoundTrip(t *testing.T) {
	secret, err := GenerateSecret()
	if err != nil {
		t.Fatalf("GenerateSecret: %v", err)
	}
	revoked := newFakeRevoked()

	user := domain.User{ID: "usr_abc", Name: "Alice", Email: "a@example.com", Status: domain.UserStatusActive, CreatedAt: time.Now()}
	tok, jti, err := NewIssuer(secret).IssueUserToken(user, time.Hour)
	if err != nil {
		t.Fatalf("IssueUserToken: %v", err)
	}
	if tok == "" || jti == "" {
		t.Fatal("empty token or jti")
	}

	claims, err := NewVerifier(secret, revoked).ParseAndValidate(context.Background(), tok)
	if err != nil {
		t.Fatalf("ParseAndValidate: %v", err)
	}
	if claims.UserID != "usr_abc" {
		t.Errorf("UserID = %q, want usr_abc", claims.UserID)
	}
	if claims.Kind != TokenKindUser {
		t.Errorf("Kind = %q, want user", claims.Kind)
	}
	if claims.ID != jti {
		t.Errorf("jti mismatch: claim=%s issued=%s", claims.ID, jti)
	}
}

func TestValidate_RejectsExpiredToken(t *testing.T) {
	secret, _ := GenerateSecret()
	revoked := newFakeRevoked()

	// Hand-construct an already-expired token.
	now := time.Now()
	claims := Claims{
		UserID: "usr_x",
		Kind:   TokenKindUser,
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        "expired-jti",
			IssuedAt:  jwt.NewNumericDate(now.Add(-2 * time.Hour)),
			ExpiresAt: jwt.NewNumericDate(now.Add(-1 * time.Hour)),
		},
	}
	tok, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(secret)

	_, err := NewVerifier(secret, revoked).ParseAndValidate(context.Background(), tok)
	if err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("got %v, want expiry error", err)
	}
}

func TestValidate_RejectsRevokedToken(t *testing.T) {
	secret, _ := GenerateSecret()
	revoked := newFakeRevoked()

	user := domain.User{ID: "usr_revoke", Name: "X", Email: "x@x.com", Status: domain.UserStatusActive}
	tok, jti, _ := NewIssuer(secret).IssueUserToken(user, time.Hour)
	revoked.revoked[jti] = true

	_, err := NewVerifier(secret, revoked).ParseAndValidate(context.Background(), tok)
	if err == nil || !strings.Contains(err.Error(), "revoked") {
		t.Fatalf("got %v, want revoked error", err)
	}
}

func TestValidate_RejectsWrongSecret(t *testing.T) {
	secret1, _ := GenerateSecret()
	secret2, _ := GenerateSecret()
	revoked := newFakeRevoked()

	user := domain.User{ID: "usr_y", Name: "Y", Email: "y@y.com", Status: domain.UserStatusActive}
	tok, _, _ := NewIssuer(secret1).IssueUserToken(user, time.Hour)

	_, err := NewVerifier(secret2, revoked).ParseAndValidate(context.Background(), tok)
	if err == nil {
		t.Fatal("expected signature mismatch error")
	}
}

func TestValidate_RejectsAlgConfusion(t *testing.T) {
	secret, _ := GenerateSecret()
	revoked := newFakeRevoked()

	// Construct an alg=none token (no signing). HS256 verifier must
	// reject this even though it's structurally valid JWT.
	claims := Claims{UserID: "attacker", RegisteredClaims: jwt.RegisteredClaims{
		ID:        "evil",
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
	}}
	tok, _ := jwt.NewWithClaims(jwt.SigningMethodNone, claims).SignedString(jwt.UnsafeAllowNoneSignatureType)

	_, err := NewVerifier(secret, revoked).ParseAndValidate(context.Background(), tok)
	if err == nil {
		t.Fatal("expected alg=none rejection")
	}
}

func TestIssueAgentToken_KindIsAgent(t *testing.T) {
	secret, _ := GenerateSecret()
	revoked := newFakeRevoked()

	tok, _, err := NewIssuer(secret).IssueAgentToken("agt_1", time.Hour)
	if err != nil {
		t.Fatalf("IssueAgentToken: %v", err)
	}
	claims, err := NewVerifier(secret, revoked).ParseAndValidate(context.Background(), tok)
	if err != nil {
		t.Fatalf("ParseAndValidate: %v", err)
	}
	if claims.Kind != TokenKindAgent {
		t.Errorf("Kind = %q, want agent", claims.Kind)
	}
}

func TestIssue_RejectsZeroTTL(t *testing.T) {
	secret, _ := GenerateSecret()
	user := domain.User{ID: "usr_z", Name: "Z", Email: "z@z.com", Status: domain.UserStatusActive}
	if _, _, err := NewIssuer(secret).IssueUserToken(user, 0); err == nil {
		t.Fatal("expected error on ttl=0")
	}
}

func TestUserToken_CarriesWildcardScope(t *testing.T) {
	secret, _ := GenerateSecret()
	revoked := newFakeRevoked()
	user := domain.User{ID: "usr_w", Name: "W", Email: "w@w.com", Status: domain.UserStatusActive}

	tok, _, err := NewIssuer(secret).IssueUserToken(user, time.Hour)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	claims, err := NewVerifier(secret, revoked).ParseAndValidate(context.Background(), tok)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if !claims.HasScope("events:write") || !claims.HasScope("anything:at:all") {
		t.Errorf("user token should grant every scope via *, got %v", claims.Scopes)
	}
}

func TestAgentTokenWithScopes_RoundTripsExactList(t *testing.T) {
	secret, _ := GenerateSecret()
	revoked := newFakeRevoked()

	tok, _, err := NewIssuer(secret).IssueAgentTokenWithScopes("agt_x", []string{"events:write", "claims:write"}, time.Hour)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	claims, err := NewVerifier(secret, revoked).ParseAndValidate(context.Background(), tok)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if !claims.HasScope("events:write") || !claims.HasScope("claims:write") {
		t.Errorf("granted scopes missing: %v", claims.Scopes)
	}
	if claims.HasScope("relationships:write") {
		t.Errorf("agent unexpectedly granted relationships:write: %v", claims.Scopes)
	}
}
