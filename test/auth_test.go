package test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/userengine/presence/pkg/auth"
)

func TestNewValidator(t *testing.T) {
	v := auth.NewValidator("test-secret")
	if v == nil {
		t.Fatal("NewValidator() returned nil")
	}
}

func TestGenerateToken(t *testing.T) {
	v := auth.NewValidator("test-secret")

	token, err := v.GenerateTokenWithExpiry("user123", []string{"scope1", "scope2"}, time.Hour)
	if err != nil {
		t.Fatalf("GenerateTokenWithExpiry() error: %v", err)
	}

	if token == "" {
		t.Error("GenerateTokenWithExpiry() returned empty token")
	}
}

func TestGenerateTokenWithoutExpiry(t *testing.T) {
	v := auth.NewValidator("test-secret")

	token, err := v.GenerateToken("user123", []string{"scope1", "scope2"})
	if err != nil {
		t.Fatalf("GenerateToken() error: %v", err)
	}

	if token == "" {
		t.Error("GenerateToken() returned empty token")
	}

	// Validate it (should work since no expiry)
	claims, err := v.ValidateToken(token)
	if err != nil {
		t.Fatalf("ValidateToken() error: %v", err)
	}

	if claims.UserID != "user123" {
		t.Errorf("claims.UserID = %q, want %q", claims.UserID, "user123")
	}
}

func TestValidateToken(t *testing.T) {
	v := auth.NewValidator("test-secret")

	// Generate a valid token
	token, err := v.GenerateTokenWithExpiry("user123", []string{"scope1", "scope2"}, time.Hour)
	if err != nil {
		t.Fatalf("GenerateTokenWithExpiry() error: %v", err)
	}

	// Validate it
	claims, err := v.ValidateToken(token)
	if err != nil {
		t.Fatalf("ValidateToken() error: %v", err)
	}

	if claims.UserID != "user123" {
		t.Errorf("claims.UserID = %q, want %q", claims.UserID, "user123")
	}

	if len(claims.Scopes) != 2 {
		t.Errorf("len(claims.Scopes) = %d, want 2", len(claims.Scopes))
	}
}

func TestValidateToken_Invalid(t *testing.T) {
	v := auth.NewValidator("test-secret")

	tests := []struct {
		name  string
		token string
	}{
		{"empty token", ""},
		{"invalid format", "not-a-jwt"},
		{"invalid signature", "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJ1c2VyX2lkIjoidGVzdCJ9.invalid"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := v.ValidateToken(tt.token)
			if err == nil {
				t.Error("expected error for invalid token")
			}
		})
	}
}

func TestValidateToken_WrongSecret(t *testing.T) {
	v1 := auth.NewValidator("secret1")
	v2 := auth.NewValidator("secret2")

	// Generate with v1
	token, _ := v1.GenerateTokenWithExpiry("user123", []string{"scope1"}, time.Hour)

	// Validate with v2 (different secret)
	_, err := v2.ValidateToken(token)
	if err == nil {
		t.Error("expected error when validating with wrong secret")
	}
}

func TestValidateToken_Expired(t *testing.T) {
	v := auth.NewValidator("test-secret")

	// Generate with negative expiry (already expired)
	token, err := v.GenerateTokenWithExpiry("user123", []string{"scope1"}, -time.Hour)
	if err != nil {
		t.Fatalf("GenerateTokenWithExpiry() error: %v", err)
	}

	_, err = v.ValidateToken(token)
	if err == nil {
		t.Error("expected error for expired token")
	}
}

func TestHasScopeAccess(t *testing.T) {
	tests := []struct {
		name    string
		scopes  []string
		scopeID string
		want    bool
	}{
		{"has access", []string{"scope1", "scope2"}, "scope1", true},
		{"no access", []string{"scope1", "scope2"}, "scope3", false},
		{"wildcard access", []string{"*"}, "any-scope", true},
		{"empty scopes", []string{}, "scope1", false},
		{"nil scopes", nil, "scope1", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			claims := &auth.Claims{Scopes: tt.scopes}
			got := claims.HasScopeAccess(tt.scopeID)
			if got != tt.want {
				t.Errorf("HasScopeAccess(%q) = %v, want %v", tt.scopeID, got, tt.want)
			}
		})
	}
}

func TestValidateRequest(t *testing.T) {
	v := auth.NewValidator("test-secret")
	token, _ := v.GenerateTokenWithExpiry("user123", []string{"scope1"}, time.Hour)

	tests := []struct {
		name       string
		authHeader string
		wantErr    bool
	}{
		{"valid bearer token", "Bearer " + token, false},
		{"missing header", "", true},
		{"missing bearer prefix", token, true},
		{"invalid token", "Bearer invalid-token", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/test", nil)
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}

			claims, err := v.ValidateRequest(req)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if claims == nil {
					t.Error("expected claims, got nil")
				}
			}
		})
	}
}

func TestValidateRequest_QueryParam(t *testing.T) {
	v := auth.NewValidator("test-secret")
	token, _ := v.GenerateTokenWithExpiry("user123", []string{"scope1"}, time.Hour)

	// Test with token in query parameter (for WebSocket connections)
	req := httptest.NewRequest("GET", "/test?token="+token, nil)

	claims, err := v.ValidateRequest(req)
	if err != nil {
		t.Fatalf("ValidateRequest() error: %v", err)
	}

	if claims.UserID != "user123" {
		t.Errorf("claims.UserID = %q, want %q", claims.UserID, "user123")
	}
}

func TestMiddleware(t *testing.T) {
	v := auth.NewValidator("test-secret")
	token, _ := v.GenerateTokenWithExpiry("user123", []string{"scope1"}, time.Hour)

	handlerCalled := false
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
		w.WriteHeader(http.StatusOK)
	})

	middleware := v.Middleware(handler)

	t.Run("valid token", func(t *testing.T) {
		handlerCalled = false
		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()

		middleware.ServeHTTP(rec, req)

		if !handlerCalled {
			t.Error("handler should have been called")
		}
		if rec.Code != http.StatusOK {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
		}
	})

	t.Run("missing token", func(t *testing.T) {
		handlerCalled = false
		req := httptest.NewRequest("GET", "/test", nil)
		rec := httptest.NewRecorder()

		middleware.ServeHTTP(rec, req)

		if handlerCalled {
			t.Error("handler should not have been called")
		}
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
		}
	})

	t.Run("invalid token", func(t *testing.T) {
		handlerCalled = false
		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("Authorization", "Bearer invalid")
		rec := httptest.NewRecorder()

		middleware.ServeHTTP(rec, req)

		if handlerCalled {
			t.Error("handler should not have been called")
		}
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
		}
	})
}

func TestClaims_JWT(t *testing.T) {
	// Test that Claims properly implements jwt.Claims interface
	claims := &auth.Claims{
		UserID: "user123",
		Scopes: []string{"scope1"},
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}

	// Should be able to get issuer (empty in our case)
	iss, _ := claims.GetIssuer()
	if iss != "" {
		t.Errorf("GetIssuer() = %q, want empty", iss)
	}

	// Expiration should be set
	exp, _ := claims.GetExpirationTime()
	if exp == nil {
		t.Error("GetExpirationTime() should not be nil")
	}
}
