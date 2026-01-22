package middleware

import (
	"net/http"
	"strings"

	"bitso-trading-platform/api-gateway/internal/logger"
)

// AuthMiddleware handles authentication
type AuthMiddleware struct {
	enabled      bool
	jwtSecret    string
	apiKeyHeader string
	logger       *logger.Logger
}

// AuthConfig holds authentication configuration
type AuthConfig struct {
	Enabled      bool
	JWTSecret    string
	APIKeyHeader string
}

// NewAuthMiddleware creates a new authentication middleware
func NewAuthMiddleware(config *AuthConfig, logger *logger.Logger) *AuthMiddleware {
	return &AuthMiddleware{
		enabled:      config.Enabled,
		jwtSecret:    config.JWTSecret,
		apiKeyHeader: config.APIKeyHeader,
		logger:       logger.WithComponent("auth"),
	}
}

// Handler returns the middleware handler function
func (m *AuthMiddleware) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip authentication if disabled
		if !m.enabled {
			next.ServeHTTP(w, r)
			return
		}

		// Skip authentication for health check endpoints
		if isHealthCheckPath(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}

		// Try API key authentication first
		apiKey := r.Header.Get(m.apiKeyHeader)
		if apiKey != "" {
			if m.validateAPIKey(apiKey) {
				// Authentication successful
				next.ServeHTTP(w, r)
				return
			}
		}

		// Try JWT authentication
		authHeader := r.Header.Get("Authorization")
		if authHeader != "" {
			token := extractBearerToken(authHeader)
			if token != "" {
				if m.validateJWT(token) {
					// Authentication successful
					next.ServeHTTP(w, r)
					return
				}
			}
		}

		// Authentication failed
		m.logger.Warn("Authentication failed", map[string]interface{}{
			"path":        r.URL.Path,
			"method":      r.Method,
			"remote_addr": r.RemoteAddr,
		})

		// Return 401 Unauthorized
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("WWW-Authenticate", "Bearer")
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":{"code":"UNAUTHORIZED","message":"Authentication required"}}`))
	})
}

// validateAPIKey validates an API key (placeholder implementation)
// TODO: Implement proper API key validation
func (m *AuthMiddleware) validateAPIKey(apiKey string) bool {
	// Placeholder: Accept any non-empty API key
	// In production, validate against a database or key management service
	m.logger.Debug("API key validation (placeholder)", map[string]interface{}{
		"key_length": len(apiKey),
	})
	return apiKey != ""
}

// validateJWT validates a JWT token (placeholder implementation)
// TODO: Implement proper JWT validation
func (m *AuthMiddleware) validateJWT(token string) bool {
	// Placeholder: Accept any non-empty token
	// In production, validate JWT signature, expiration, claims, etc.
	m.logger.Debug("JWT validation (placeholder)", map[string]interface{}{
		"token_length": len(token),
	})
	return token != ""
}

// extractBearerToken extracts the token from "Bearer <token>" format
func extractBearerToken(authHeader string) string {
	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) == 2 && strings.ToLower(parts[0]) == "bearer" {
		return parts[1]
	}
	return ""
}

// isHealthCheckPath checks if the path is a health check endpoint
func isHealthCheckPath(path string) bool {
	healthPaths := []string{
		"/health",
		"/health/live",
		"/health/ready",
		"/metrics",
	}

	for _, healthPath := range healthPaths {
		if path == healthPath {
			return true
		}
	}

	return false
}
