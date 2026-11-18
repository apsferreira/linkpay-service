package jwt

import (
	"crypto/rand"
	"crypto/rsa"
	"strings"
	"testing"
	"time"

	jwttoken "github.com/golang-jwt/jwt/v5"
)

const covJWTSecret = "cov-jwt-secret"

// covNewClaims builds a fully populated claim set.
func covNewClaims() *Claims {
	return &Claims{
		RegisteredClaims: jwttoken.RegisteredClaims{
			Subject:   "6f1b6b3e-6a1f-4f0b-9c11-2f2a2f2a2f2a",
			Issuer:    "auth-service",
			ExpiresAt: jwttoken.NewNumericDate(time.Now().Add(time.Hour)),
			IssuedAt:  jwttoken.NewNumericDate(time.Now()),
		},
		TenantID:    "tenant-42",
		Email:       "user@example.com",
		Roles:       []string{"user", "admin"},
		Permissions: map[string][]string{"links": {"read", "write"}},
	}
}

// covSign signs claims with the given method and key.
func covSign(t *testing.T, method jwttoken.SigningMethod, key any, claims jwttoken.Claims) string {
	t.Helper()
	signed, err := jwttoken.NewWithClaims(method, claims).SignedString(key)
	if err != nil {
		t.Fatalf("SignedString: %v", err)
	}
	return signed
}

func TestCovJWTServiceValidateValidToken(t *testing.T) {
	svc := NewJWTService(covJWTSecret)
	token := covSign(t, jwttoken.SigningMethodHS256, []byte(covJWTSecret), covNewClaims())

	claims, err := svc.Validate(token)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if claims.Subject != "6f1b6b3e-6a1f-4f0b-9c11-2f2a2f2a2f2a" {
		t.Errorf("Subject = %q", claims.Subject)
	}
	if claims.TenantID != "tenant-42" {
		t.Errorf("TenantID = %q", claims.TenantID)
	}
	if claims.Email != "user@example.com" {
		t.Errorf("Email = %q", claims.Email)
	}
	if len(claims.Roles) != 2 || claims.Roles[0] != "user" || claims.Roles[1] != "admin" {
		t.Errorf("Roles = %v", claims.Roles)
	}
	if len(claims.Permissions["links"]) != 2 {
		t.Errorf("Permissions = %v", claims.Permissions)
	}
	if claims.Issuer != "auth-service" {
		t.Errorf("Issuer = %q", claims.Issuer)
	}
}

func TestCovJWTServiceValidateHS512Token(t *testing.T) {
	svc := NewJWTService(covJWTSecret)
	token := covSign(t, jwttoken.SigningMethodHS512, []byte(covJWTSecret), covNewClaims())

	claims, err := svc.Validate(token)
	if err != nil {
		t.Fatalf("Validate HS512: %v", err)
	}
	if claims.TenantID != "tenant-42" {
		t.Errorf("TenantID = %q", claims.TenantID)
	}
}

func TestCovJWTServiceValidateEmptyClaims(t *testing.T) {
	svc := NewJWTService(covJWTSecret)
	token := covSign(t, jwttoken.SigningMethodHS256, []byte(covJWTSecret), &Claims{})

	claims, err := svc.Validate(token)
	if err != nil {
		t.Fatalf("Validate empty claims: %v", err)
	}
	if claims.Subject != "" {
		t.Errorf("Subject = %q, want empty", claims.Subject)
	}
	if claims.TenantID != "" || claims.Email != "" || claims.Roles != nil {
		t.Errorf("expected zero-value claims, got %+v", claims)
	}
}

func TestCovJWTServiceValidateWrongSecret(t *testing.T) {
	svc := NewJWTService(covJWTSecret)
	token := covSign(t, jwttoken.SigningMethodHS256, []byte("another-secret"), covNewClaims())

	if _, err := svc.Validate(token); err == nil {
		t.Fatal("expected error for token signed with a different secret")
	} else if !strings.Contains(err.Error(), "invalid token") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestCovJWTServiceValidateExpiredToken(t *testing.T) {
	svc := NewJWTService(covJWTSecret)

	claims := covNewClaims()
	claims.ExpiresAt = jwttoken.NewNumericDate(time.Now().Add(-time.Hour))
	token := covSign(t, jwttoken.SigningMethodHS256, []byte(covJWTSecret), claims)

	if _, err := svc.Validate(token); err == nil {
		t.Fatal("expected error for expired token")
	}
}

func TestCovJWTServiceValidateNotBefore(t *testing.T) {
	svc := NewJWTService(covJWTSecret)

	claims := covNewClaims()
	claims.NotBefore = jwttoken.NewNumericDate(time.Now().Add(time.Hour))
	token := covSign(t, jwttoken.SigningMethodHS256, []byte(covJWTSecret), claims)

	if _, err := svc.Validate(token); err == nil {
		t.Fatal("expected error for token that is not valid yet")
	}
}

func TestCovJWTServiceValidateNoneAlg(t *testing.T) {
	svc := NewJWTService(covJWTSecret)
	token := covSign(t, jwttoken.SigningMethodNone, jwttoken.UnsafeAllowNoneSignatureType, covNewClaims())

	_, err := svc.Validate(token)
	if err == nil {
		t.Fatal("expected error for alg=none token")
	}
	if !strings.Contains(err.Error(), "unexpected signing method") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestCovJWTServiceValidateRS256Alg(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa.GenerateKey: %v", err)
	}

	svc := NewJWTService(covJWTSecret)
	token := covSign(t, jwttoken.SigningMethodRS256, key, covNewClaims())

	_, err = svc.Validate(token)
	if err == nil {
		t.Fatal("expected error for RS256 token")
	}
	if !strings.Contains(err.Error(), "unexpected signing method") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestCovJWTServiceValidateMalformedToken(t *testing.T) {
	svc := NewJWTService(covJWTSecret)

	cases := []string{
		"",
		"not-a-token",
		"header.payload",
		"a.b.c",
		strings.Repeat("x", 128),
	}
	for _, tc := range cases {
		if _, err := svc.Validate(tc); err == nil {
			t.Errorf("expected error for malformed token %q", tc)
		}
	}
}

func TestCovValidateTokenDirect(t *testing.T) {
	token := covSign(t, jwttoken.SigningMethodHS384, []byte(covJWTSecret), covNewClaims())

	claims, err := ValidateToken(token, covJWTSecret)
	if err != nil {
		t.Fatalf("ValidateToken: %v", err)
	}
	if claims.Email != "user@example.com" {
		t.Errorf("Email = %q", claims.Email)
	}

	if _, err := ValidateToken(token, "wrong-secret"); err == nil {
		t.Fatal("expected error with the wrong secret")
	}
}

func TestCovNewJWTService(t *testing.T) {
	svc := NewJWTService(covJWTSecret)
	if svc == nil {
		t.Fatal("NewJWTService returned nil")
	}
	if svc.secret != covJWTSecret {
		t.Errorf("secret = %q, want %q", svc.secret, covJWTSecret)
	}
}
