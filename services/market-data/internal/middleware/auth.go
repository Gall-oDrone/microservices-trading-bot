package middleware

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// AuthConfig holds authentication configuration
type AuthConfig struct {
	APIKey    string
	SecretKey string
	Enabled   bool
}

// AuthMiddleware creates an authentication middleware
func AuthMiddleware(config *AuthConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Skip auth for health checks and public endpoints
			if isPublicEndpoint(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}

			// Skip auth if disabled
			if !config.Enabled {
				next.ServeHTTP(w, r)
				return
			}

			// Extract API key from header
			apiKey := r.Header.Get("X-API-Key")
			if apiKey == "" {
				http.Error(w, "Missing API key", http.StatusUnauthorized)
				return
			}

			// Validate API key
			if apiKey != config.APIKey {
				http.Error(w, "Invalid API key", http.StatusUnauthorized)
				return
			}

			// Extract signature from header
			signature := r.Header.Get("X-Signature")
			if signature == "" {
				http.Error(w, "Missing signature", http.StatusUnauthorized)
				return
			}

			// Validate signature
			if !validateSignature(r, signature, config.SecretKey) {
				http.Error(w, "Invalid signature", http.StatusUnauthorized)
				return
			}

			// Add user context
			ctx := context.WithValue(r.Context(), "api_key", apiKey)
			r = r.WithContext(ctx)

			next.ServeHTTP(w, r)
		})
	}
}

// isPublicEndpoint checks if the endpoint is public
func isPublicEndpoint(path string) bool {
	publicPaths := []string{
		"/health",
		"/health/live",
		"/health/ready",
		"/metrics",
	}

	for _, publicPath := range publicPaths {
		if strings.HasPrefix(path, publicPath) {
			return true
		}
	}

	return false
}

// validateSignature validates the request signature
func validateSignature(r *http.Request, signature string, secretKey string) bool {
	// Create the message to sign
	message := createMessage(r)

	// Calculate the expected signature
	expectedSignature := calculateSignature(message, secretKey)

	// Compare signatures
	return hmac.Equal([]byte(signature), []byte(expectedSignature))
}

// createMessage creates the message to sign
func createMessage(r *http.Request) string {
	var parts []string

	// Add method
	parts = append(parts, r.Method)

	// Add path
	parts = append(parts, r.URL.Path)

	// Add query string
	if r.URL.RawQuery != "" {
		parts = append(parts, r.URL.RawQuery)
	}

	// Add timestamp
	timestamp := r.Header.Get("X-Timestamp")
	if timestamp == "" {
		timestamp = fmt.Sprintf("%d", time.Now().Unix())
	}
	parts = append(parts, timestamp)

	// Add nonce
	nonce := r.Header.Get("X-Nonce")
	if nonce == "" {
		nonce = fmt.Sprintf("%d", time.Now().UnixNano())
	}
	parts = append(parts, nonce)

	return strings.Join(parts, "|")
}

// calculateSignature calculates the HMAC signature
func calculateSignature(message, secretKey string) string {
	h := hmac.New(sha256.New, []byte(secretKey))
	h.Write([]byte(message))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

// GetAPIKeyFromContext extracts the API key from the request context
func GetAPIKeyFromContext(ctx context.Context) (string, bool) {
	apiKey, ok := ctx.Value("api_key").(string)
	return apiKey, ok
}
