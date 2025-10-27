package middleware

import (
	"net/http"
	"strings"
)

// CORSMiddleware handles CORS (Cross-Origin Resource Sharing) requests
type CORSMiddleware struct {
	allowedOrigins   []string
	allowedMethods   []string
	allowedHeaders   []string
	exposedHeaders   []string
	allowCredentials bool
	maxAge           int
}

// CORSConfig holds CORS configuration
type CORSConfig struct {
	AllowedOrigins   []string
	AllowedMethods   []string
	AllowedHeaders   []string
	ExposedHeaders   []string
	AllowCredentials bool
	MaxAge           int // In seconds
}

// DefaultCORSConfig returns default CORS configuration
func DefaultCORSConfig() *CORSConfig {
	return &CORSConfig{
		AllowedOrigins: []string{"*"},
		AllowedMethods: []string{
			http.MethodGet,
			http.MethodPost,
			http.MethodPut,
			http.MethodDelete,
			http.MethodOptions,
			http.MethodPatch,
		},
		AllowedHeaders: []string{
			"Accept",
			"Authorization",
			"Content-Type",
			"X-Request-ID",
			"X-API-Key",
		},
		ExposedHeaders: []string{
			"X-Request-ID",
			"X-RateLimit-Limit",
			"X-RateLimit-Remaining",
			"X-RateLimit-Reset",
		},
		AllowCredentials: false,
		MaxAge:           3600, // 1 hour
	}
}

// NewCORSMiddleware creates a new CORS middleware
func NewCORSMiddleware(config *CORSConfig) *CORSMiddleware {
	if config == nil {
		config = DefaultCORSConfig()
	}

	return &CORSMiddleware{
		allowedOrigins:   config.AllowedOrigins,
		allowedMethods:   config.AllowedMethods,
		allowedHeaders:   config.AllowedHeaders,
		exposedHeaders:   config.ExposedHeaders,
		allowCredentials: config.AllowCredentials,
		maxAge:           config.MaxAge,
	}
}

// Handler returns the middleware handler function
func (m *CORSMiddleware) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Get the origin from the request
		origin := r.Header.Get("Origin")

		// Check if origin is allowed
		if m.isOriginAllowed(origin) {
			// Set CORS headers
			w.Header().Set("Access-Control-Allow-Origin", origin)
		} else if m.allowAll() {
			// Allow all origins
			w.Header().Set("Access-Control-Allow-Origin", "*")
		}

		// Set other CORS headers
		if len(m.allowedMethods) > 0 {
			w.Header().Set("Access-Control-Allow-Methods", strings.Join(m.allowedMethods, ", "))
		}

		if len(m.allowedHeaders) > 0 {
			w.Header().Set("Access-Control-Allow-Headers", strings.Join(m.allowedHeaders, ", "))
		}

		if len(m.exposedHeaders) > 0 {
			w.Header().Set("Access-Control-Expose-Headers", strings.Join(m.exposedHeaders, ", "))
		}

		if m.allowCredentials {
			w.Header().Set("Access-Control-Allow-Credentials", "true")
		}

		if m.maxAge > 0 {
			w.Header().Set("Access-Control-Max-Age", string(rune(m.maxAge)))
		}

		// Handle preflight requests
		if r.Method == http.MethodOptions {
			// Preflight request
			w.WriteHeader(http.StatusNoContent)
			return
		}

		// Call next handler
		next.ServeHTTP(w, r)
	})
}

// isOriginAllowed checks if the origin is in the allowed list
func (m *CORSMiddleware) isOriginAllowed(origin string) bool {
	if origin == "" {
		return false
	}

	for _, allowed := range m.allowedOrigins {
		if allowed == origin {
			return true
		}
		// Support wildcard subdomains (e.g., "*.example.com")
		if strings.HasPrefix(allowed, "*.") {
			domain := strings.TrimPrefix(allowed, "*.")
			if strings.HasSuffix(origin, domain) {
				return true
			}
		}
	}

	return false
}

// allowAll checks if all origins are allowed
func (m *CORSMiddleware) allowAll() bool {
	for _, origin := range m.allowedOrigins {
		if origin == "*" {
			return true
		}
	}
	return false
}
