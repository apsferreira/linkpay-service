package middleware

import (
	"encoding/json"
	"errors"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	jwttoken "github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"linkpay-service/pkg/jwt"
)

const covSecret = "cov-test-secret"

// covSignToken builds an HS256 token signed with secret using the service Claims type.
func covSignToken(t *testing.T, secret string, claims *jwt.Claims) string {
	t.Helper()
	tok := jwttoken.NewWithClaims(jwttoken.SigningMethodHS256, claims)
	signed, err := tok.SignedString([]byte(secret))
	if err != nil {
		t.Fatalf("covSignToken: %v", err)
	}
	return signed
}

// covClaims returns a valid claim set for the given subject.
func covClaims(subject string) *jwt.Claims {
	return &jwt.Claims{
		RegisteredClaims: jwttoken.RegisteredClaims{
			Subject:   subject,
			ExpiresAt: jwttoken.NewNumericDate(time.Now().Add(time.Hour)),
			IssuedAt:  jwttoken.NewNumericDate(time.Now()),
		},
		TenantID:    "tenant-1",
		Email:       "user@example.com",
		Roles:       []string{"user"},
		Permissions: map[string][]string{"links": {"read"}},
	}
}

// covLocalsHandler echoes the Locals set by the auth middleware.
func covLocalsHandler(c *fiber.Ctx) error {
	out := map[string]any{}
	if v := c.Locals("customer_id"); v != nil {
		out["customer_id"] = v.(uuid.UUID).String()
	}
	if v := c.Locals("user_id"); v != nil {
		out["user_id"] = v.(string)
	}
	if v := c.Locals("tenant_id"); v != nil {
		out["tenant_id"] = v.(string)
	}
	if v := c.Locals("email"); v != nil {
		out["email"] = v.(string)
	}
	if v := c.Locals("roles"); v != nil {
		out["roles"] = v.([]string)
	}
	return c.JSON(out)
}

// covRequest performs a GET against app, optionally setting the Authorization header.
func covRequest(t *testing.T, app *fiber.App, path, authorization string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest("GET", path, nil)
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test(%s): %v", path, err)
	}
	defer resp.Body.Close()

	var body map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&body)
	return resp.StatusCode, body
}

// covAuthApp builds an app protected by JWTAuth(svc).
func covAuthApp(svc *jwt.JWTService) *fiber.App {
	app := fiber.New()
	app.Use(JWTAuth(svc))
	app.Get("/", covLocalsHandler)
	return app
}

func TestCovJWTAuthMissingHeader(t *testing.T) {
	app := covAuthApp(jwt.NewJWTService(covSecret))

	status, body := covRequest(t, app, "/", "")
	if status != fiber.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", status)
	}
	if body["error"] == nil {
		t.Fatalf("expected error message, got %v", body)
	}
}

func TestCovJWTAuthHeaderWithoutBearerPrefix(t *testing.T) {
	app := covAuthApp(jwt.NewJWTService(covSecret))

	for _, header := range []string{"Token abc", "Basic dXNlcjpwYXNz", covSignToken(t, covSecret, covClaims(uuid.NewString()))} {
		status, _ := covRequest(t, app, "/", header)
		if status != fiber.StatusUnauthorized {
			t.Fatalf("header %q: expected 401, got %d", header, status)
		}
	}
}

func TestCovJWTAuthInvalidToken(t *testing.T) {
	app := covAuthApp(jwt.NewJWTService(covSecret))

	cases := []string{
		"Bearer not-a-token",
		"Bearer " + covSignToken(t, "other-secret", covClaims(uuid.NewString())),
		"Bearer ",
	}
	for _, header := range cases {
		status, _ := covRequest(t, app, "/", header)
		if status != fiber.StatusUnauthorized {
			t.Fatalf("header %q: expected 401, got %d", header, status)
		}
	}
}

func TestCovJWTAuthValidTokenUUIDSubject(t *testing.T) {
	app := covAuthApp(jwt.NewJWTService(covSecret))

	customerID := uuid.NewString()
	header := "Bearer " + covSignToken(t, covSecret, covClaims(customerID))

	status, body := covRequest(t, app, "/", header)
	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d (%v)", status, body)
	}
	if got := body["customer_id"]; got != customerID {
		t.Errorf("customer_id = %v, want %s", got, customerID)
	}
	if got := body["user_id"]; got != customerID {
		t.Errorf("user_id = %v, want %s", got, customerID)
	}
	if got := body["tenant_id"]; got != "tenant-1" {
		t.Errorf("tenant_id = %v, want tenant-1", got)
	}
	if got := body["email"]; got != "user@example.com" {
		t.Errorf("email = %v, want user@example.com", got)
	}
	roles, ok := body["roles"].([]any)
	if !ok || len(roles) != 1 || roles[0] != "user" {
		t.Errorf("roles = %v, want [user]", body["roles"])
	}
}

func TestCovJWTAuthValidTokenNonUUIDSubject(t *testing.T) {
	app := covAuthApp(jwt.NewJWTService(covSecret))

	header := "Bearer " + covSignToken(t, covSecret, covClaims("legacy-user-123"))

	status, body := covRequest(t, app, "/", header)
	if status != fiber.StatusOK {
		t.Fatalf("expected 200 for non-UUID subject, got %d (%v)", status, body)
	}
	if _, exists := body["customer_id"]; exists {
		t.Errorf("customer_id should not be set for non-UUID subject, got %v", body["customer_id"])
	}
	if got := body["user_id"]; got != "legacy-user-123" {
		t.Errorf("user_id = %v, want legacy-user-123", got)
	}
}

// covMWApp builds an app protected by JWTMiddleware(secret).
func covMWApp(secret string) *fiber.App {
	app := fiber.New()
	app.Use(JWTMiddleware(secret))
	app.Get("/", covLocalsHandler)
	return app
}

func TestCovJWTMiddlewareMissingHeader(t *testing.T) {
	app := covMWApp(covSecret)

	for _, header := range []string{"", "Token abc"} {
		status, body := covRequest(t, app, "/", header)
		if status != fiber.StatusUnauthorized {
			t.Fatalf("header %q: expected 401, got %d", header, status)
		}
		if body["error"] != "missing authorization header" {
			t.Errorf("header %q: unexpected error message %v", header, body["error"])
		}
	}
}

func TestCovJWTMiddlewareInvalidToken(t *testing.T) {
	app := covMWApp(covSecret)

	status, body := covRequest(t, app, "/", "Bearer garbage.token.value")
	if status != fiber.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", status)
	}
	if body["error"] != "invalid or expired token" {
		t.Errorf("unexpected error message %v", body["error"])
	}
}

func TestCovJWTMiddlewareNonUUIDSubject(t *testing.T) {
	app := covMWApp(covSecret)

	header := "Bearer " + covSignToken(t, covSecret, covClaims("not-a-uuid"))
	status, body := covRequest(t, app, "/", header)
	if status != fiber.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", status)
	}
	if body["error"] != "invalid customer ID in token" {
		t.Errorf("unexpected error message %v", body["error"])
	}
}

func TestCovJWTMiddlewareValidToken(t *testing.T) {
	app := covMWApp(covSecret)

	customerID := uuid.NewString()
	header := "Bearer " + covSignToken(t, covSecret, covClaims(customerID))
	status, body := covRequest(t, app, "/", header)
	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d (%v)", status, body)
	}
	if got := body["customer_id"]; got != customerID {
		t.Errorf("customer_id = %v, want %s", got, customerID)
	}
	if got := body["user_id"]; got != customerID {
		t.Errorf("user_id = %v, want %s", got, customerID)
	}
	if got := body["tenant_id"]; got != "tenant-1" {
		t.Errorf("tenant_id = %v, want tenant-1", got)
	}
	if got := body["email"]; got != "user@example.com" {
		t.Errorf("email = %v, want user@example.com", got)
	}
}

// covRoleApp builds an app that seeds Locals("roles") with seed (nil means unset)
// and then guards the route with RequireRole(required...).
func covRoleApp(seed any, required ...string) *fiber.App {
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		if seed != nil {
			c.Locals("roles", seed)
		}
		return c.Next()
	})
	app.Use(RequireRole(required...))
	app.Get("/", func(c *fiber.Ctx) error { return c.SendString("ok") })
	return app
}

func TestCovRequireRoleMissingLocals(t *testing.T) {
	app := covRoleApp(nil, "admin")

	status, body := covRequest(t, app, "/", "")
	if status != fiber.StatusForbidden {
		t.Fatalf("expected 403, got %d", status)
	}
	if body["error"] != "forbidden" {
		t.Errorf("unexpected error message %v", body["error"])
	}
}

func TestCovRequireRoleInsufficientRole(t *testing.T) {
	app := covRoleApp([]string{"user"}, "admin")

	status, body := covRequest(t, app, "/", "")
	if status != fiber.StatusForbidden {
		t.Fatalf("expected 403, got %d", status)
	}
	if body["error"] != "insufficient permissions" {
		t.Errorf("unexpected error message %v", body["error"])
	}
}

func TestCovRequireRoleAllowed(t *testing.T) {
	app := covRoleApp([]string{"user", "admin"}, "admin")

	status, _ := covRequest(t, app, "/", "")
	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d", status)
	}
}

func TestCovRequireRoleMultipleRoles(t *testing.T) {
	app := covRoleApp([]string{"user", "support", "admin"}, "admin", "support", "superadmin")

	status, _ := covRequest(t, app, "/", "")
	if status != fiber.StatusOK {
		t.Fatalf("expected 200, got %d", status)
	}
}

func TestCovRequireRoleMultipleRolesDenied(t *testing.T) {
	app := covRoleApp([]string{"user", "viewer"}, "admin", "support")

	status, _ := covRequest(t, app, "/", "")
	if status != fiber.StatusForbidden {
		t.Fatalf("expected 403, got %d", status)
	}
}

func TestCovErrorHandlerFiberError(t *testing.T) {
	app := fiber.New(fiber.Config{ErrorHandler: ErrorHandler})
	app.Get("/teapot", func(c *fiber.Ctx) error {
		return fiber.NewError(fiber.StatusTeapot, "i am a teapot")
	})

	status, body := covRequest(t, app, "/teapot", "")
	if status != fiber.StatusTeapot {
		t.Fatalf("expected 418, got %d", status)
	}
	if body["error"] != "i am a teapot" {
		t.Errorf("error = %v, want 'i am a teapot'", body["error"])
	}
	if got, ok := body["status"].(float64); !ok || int(got) != fiber.StatusTeapot {
		t.Errorf("status = %v, want 418", body["status"])
	}
}

func TestCovErrorHandlerGenericError(t *testing.T) {
	app := fiber.New(fiber.Config{ErrorHandler: ErrorHandler})
	app.Get("/boom", func(c *fiber.Ctx) error {
		return errors.New("boom")
	})

	status, body := covRequest(t, app, "/boom", "")
	if status != fiber.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", status)
	}
	if body["error"] != "Internal Server Error" {
		t.Errorf("error = %v, want Internal Server Error", body["error"])
	}
	if got, ok := body["status"].(float64); !ok || int(got) != 500 {
		t.Errorf("status = %v, want 500", body["status"])
	}
}

func TestCovSecurityHeaders(t *testing.T) {
	app := fiber.New()
	app.Use(SecurityHeaders())
	app.Get("/", func(c *fiber.Ctx) error { return c.SendString("ok") })

	req := httptest.NewRequest("GET", "/", nil)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	expected := map[string]string{
		"X-Content-Type-Options":    "nosniff",
		"X-Frame-Options":           "DENY",
		"X-XSS-Protection":          "0",
		"Strict-Transport-Security": "max-age=31536000; includeSubDomains",
		"Referrer-Policy":           "strict-origin-when-cross-origin",
		"Content-Security-Policy":   "default-src 'self'",
	}
	for header, want := range expected {
		if got := resp.Header.Get(header); got != want {
			t.Errorf("header %s = %q, want %q", header, got, want)
		}
	}
}
