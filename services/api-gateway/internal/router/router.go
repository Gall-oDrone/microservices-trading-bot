package router

import (
	"net/http"

	"bitso-trading-platform/api-gateway/internal/logger"
)

// Router wraps http.ServeMux with middleware support
type Router struct {
	mux        *http.ServeMux
	middleware []func(http.Handler) http.Handler
	logger     *logger.Logger
}

// NewRouter creates a new router
func NewRouter(logger *logger.Logger) *Router {
	return &Router{
		mux:        http.NewServeMux(),
		middleware: make([]func(http.Handler) http.Handler, 0),
		logger:     logger.WithComponent("router"),
	}
}

// Use adds middleware to the router
func (r *Router) Use(middleware func(http.Handler) http.Handler) {
	r.middleware = append(r.middleware, middleware)
}

// GetMux returns the underlying ServeMux
func (r *Router) GetMux() *http.ServeMux {
	return r.mux
}

// Handler returns the final handler with all middleware applied
func (r *Router) Handler() http.Handler {
	// Start with the mux
	handler := http.Handler(r.mux)

	// Apply middleware in reverse order (so first Use() is outermost)
	for i := len(r.middleware) - 1; i >= 0; i-- {
		handler = r.middleware[i](handler)
	}

	return handler
}

// HandleFunc registers a handler function
func (r *Router) HandleFunc(pattern string, handler http.HandlerFunc) {
	r.mux.HandleFunc(pattern, handler)
}

// Handle registers a handler
func (r *Router) Handle(pattern string, handler http.Handler) {
	r.mux.Handle(pattern, handler)
}

// chainMiddleware chains multiple middleware together
func chainMiddleware(h http.Handler, middleware ...func(http.Handler) http.Handler) http.Handler {
	// Apply middleware in reverse order
	for i := len(middleware) - 1; i >= 0; i-- {
		h = middleware[i](h)
	}
	return h
}

