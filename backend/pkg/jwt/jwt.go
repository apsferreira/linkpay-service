package jwt

import (
	"fmt"

	"github.com/golang-jwt/jwt/v5"
)

// Claims mirrors auth-service JWT structure
type Claims struct {
	jwt.RegisteredClaims
	TenantID    string              `json:"tenant_id"`
	Email       string              `json:"email"`
	Roles       []string            `json:"roles"`
	Permissions map[string][]string `json:"permissions"`
}

func ValidateToken(tokenString, secret string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return []byte(secret), nil
	})
	if err != nil || !token.Valid {
		return nil, fmt.Errorf("invalid token: %w", err)
	}
	claims, ok := token.Claims.(*Claims)
	if !ok {
		return nil, fmt.Errorf("invalid token claims")
	}
	return claims, nil
}