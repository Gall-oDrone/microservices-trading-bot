// Package httpcors is the one CORS policy for the platform's HTTP services
// (plan §7 item 2): an explicit origin allowlist from CORS_ALLOWED_ORIGINS,
// no wildcard. With the variable unset no CORS headers are sent at all, so
// browsers only reach a service same-origin (the operator UI goes through
// ui-api, which is same-origin and sets no CORS headers either).
package httpcors

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// EnvAllowedOrigins is a comma-separated list of exact origins, e.g.
// "https://ops.example.com,http://127.0.0.1:5173".
const EnvAllowedOrigins = "CORS_ALLOWED_ORIGINS"

// Policy is a parsed allowlist. The zero value and nil allow no origin.
type Policy struct {
	origins map[string]bool
	// Methods and Headers are sent on allowed preflights.
	Methods string
	Headers string
	MaxAge  int // seconds
}

// Parse builds a Policy from a comma-separated origin list. Each entry must
// be scheme://host[:port] with scheme http or https, no path, query or
// wildcard; "*" is refused because the platform never wants any-origin.
func Parse(list string) (*Policy, error) {
	p := &Policy{
		origins: map[string]bool{},
		Methods: "GET, POST, PUT, DELETE, OPTIONS",
		Headers: "Content-Type, Authorization, X-Request-ID",
		MaxAge:  600,
	}
	for _, raw := range strings.Split(list, ",") {
		o := strings.TrimSpace(raw)
		if o == "" {
			continue
		}
		if strings.Contains(o, "*") {
			return nil, fmt.Errorf("%s: wildcard origin %q is not allowed; list exact origins", EnvAllowedOrigins, o)
		}
		u, err := url.Parse(o)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" ||
			(u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
			return nil, fmt.Errorf("%s: %q is not an origin (want scheme://host[:port])", EnvAllowedOrigins, o)
		}
		p.origins[strings.ToLower(u.Scheme+"://"+u.Host)] = true
	}
	return p, nil
}

// FromEnv reads EnvAllowedOrigins.
func FromEnv(getenv func(string) string) (*Policy, error) {
	return Parse(getenv(EnvAllowedOrigins))
}

// Origins returns the allowlist (for start-up logs).
func (p *Policy) Origins() []string {
	if p == nil {
		return nil
	}
	out := make([]string, 0, len(p.origins))
	for o := range p.origins {
		out = append(out, o)
	}
	return out
}

// Allowed reports whether origin is on the list (exact, case-insensitive).
func (p *Policy) Allowed(origin string) bool {
	return p != nil && origin != "" && p.origins[strings.ToLower(origin)]
}

// Middleware applies the policy. Requests without an Origin header pass
// untouched. An allowed origin gets Access-Control-Allow-Origin set to
// itself (never "*") and preflights answered 204. A preflight from any other
// origin is refused with 403; other requests from it pass without CORS
// headers, so the browser will not let the page read the response.
func (p *Policy) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin == "" {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Add("Vary", "Origin")
		preflight := r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != ""
		if !p.Allowed(origin) {
			if preflight {
				http.Error(w, "origin not allowed", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
			return
		}
		h := w.Header()
		h.Set("Access-Control-Allow-Origin", origin)
		if preflight {
			h.Set("Access-Control-Allow-Methods", p.Methods)
			h.Set("Access-Control-Allow-Headers", p.Headers)
			if p.MaxAge > 0 {
				h.Set("Access-Control-Max-Age", strconv.Itoa(p.MaxAge))
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
