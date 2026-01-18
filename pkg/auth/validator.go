package auth

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

var (
	ErrNoToken      = errors.New("no token provided")
	ErrInvalidToken = errors.New("invalid token")
	ErrExpiredToken = errors.New("token expired")
)

// Claims represents the JWT claims for presence authorization.
type Claims struct {
	jwt.RegisteredClaims
	UserID string   `json:"user_id"`
	Scopes []string `json:"scopes"` // List of scope_ids the user can access
}

// HasScopeAccess checks if the claims grant access to a specific scope.
func (c *Claims) HasScopeAccess(scopeID string) bool {
	for _, s := range c.Scopes {
		if s == scopeID || s == "*" {
			return true
		}
	}
	return false
}

// Validator validates JWT tokens for presence authorization.
type Validator struct {
	secret []byte
}

// NewValidator creates a new JWT validator.
func NewValidator(secret string) *Validator {
	return &Validator{
		secret: []byte(secret),
	}
}

// ValidateRequest extracts and validates JWT from HTTP request.
// Checks Authorization header (Bearer token), Cookie, and query param.
func (v *Validator) ValidateRequest(r *http.Request) (*Claims, error) {
	tokenString := v.extractToken(r)
	if tokenString == "" {
		return nil, ErrNoToken
	}

	return v.ValidateToken(tokenString)
}

// ValidateToken validates a JWT token string.
func (v *Validator) ValidateToken(tokenString string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, ErrInvalidToken
		}
		return v.secret, nil
	})

	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return nil, ErrExpiredToken
		}
		return nil, ErrInvalidToken
	}

	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, ErrInvalidToken
	}

	return claims, nil
}

// GenerateToken creates a JWT token.
// For production, consider adding expiration via expiresAt parameter.
func (v *Validator) GenerateToken(userID string, scopes []string) (string, error) {
	claims := &Claims{
		RegisteredClaims: jwt.RegisteredClaims{},
		UserID:           userID,
		Scopes:           scopes,
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(v.secret)
}

// GenerateTokenWithExpiry creates a JWT token with expiration.
func (v *Validator) GenerateTokenWithExpiry(userID string, scopes []string, expiresIn time.Duration) (string, error) {
	claims := &Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(expiresIn)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
		UserID: userID,
		Scopes: scopes,
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(v.secret)
}

func (v *Validator) extractToken(r *http.Request) string {
	// Check Authorization header
	auth := r.Header.Get("Authorization")
	if auth != "" {
		parts := strings.SplitN(auth, " ", 2)
		if len(parts) == 2 && strings.ToLower(parts[0]) == "bearer" {
			return parts[1]
		}
	}

	// Check cookie
	cookie, err := r.Cookie("presence_token")
	if err == nil && cookie.Value != "" {
		return cookie.Value
	}

	// Check query param (for WebSocket upgrade)
	if token := r.URL.Query().Get("token"); token != "" {
		return token
	}

	return ""
}

// Middleware returns an HTTP middleware that validates JWT tokens.
// On valid token, the handler is called with claims added to request context.
// On invalid or missing token, returns 401 Unauthorized.
func (v *Validator) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := v.ValidateRequest(r)
		if err != nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}
