package httpcors

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestParse(t *testing.T) {
	p, err := Parse(" https://ops.example.com , http://127.0.0.1:5173,HTTPS://UPPER.example.com/ ")
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range []string{"https://ops.example.com", "http://127.0.0.1:5173", "https://upper.example.com", "HTTPS://OPS.EXAMPLE.COM"} {
		if !p.Allowed(o) {
			t.Errorf("%s should be allowed", o)
		}
	}
	for _, o := range []string{"", "https://evil-ops.example.com", "http://ops.example.com", "https://ops.example.com.evil.io", "null"} {
		if p.Allowed(o) {
			t.Errorf("%s should not be allowed", o)
		}
	}
	for _, bad := range []string{"*", "https://*.example.com", "ops.example.com", "ftp://x.io", "https://x.io/path", "https://x.io?q=1", "https://u:p@x.io"} {
		if _, err := Parse(bad); err == nil {
			t.Errorf("Parse(%q) accepted", bad)
		}
	}
	empty, err := FromEnv(func(string) string { return "" })
	if err != nil || empty.Allowed("http://127.0.0.1:5173") || len(empty.Origins()) != 0 {
		t.Fatal("unset env must allow nothing")
	}
	var nilP *Policy
	if nilP.Allowed("http://x.io") || nilP.Origins() != nil {
		t.Fatal("nil policy must allow nothing")
	}
}

func TestMiddleware(t *testing.T) {
	p, _ := Parse("https://ops.example.com")
	called := 0
	h := p.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called++
		w.WriteHeader(http.StatusTeapot)
	}))
	do := func(method, origin string, preflight bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "/api/v1/ticker", nil)
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		if preflight {
			r.Header.Set("Access-Control-Request-Method", "POST")
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}

	// No Origin: untouched.
	w := do(http.MethodGet, "", false)
	if w.Code != http.StatusTeapot || w.Header().Get("Access-Control-Allow-Origin") != "" || w.Header().Get("Vary") != "" {
		t.Fatalf("no-origin request changed: %d %v", w.Code, w.Header())
	}
	// Allowed origin: echoed, never "*".
	w = do(http.MethodGet, "https://ops.example.com", false)
	if w.Code != http.StatusTeapot || w.Header().Get("Access-Control-Allow-Origin") != "https://ops.example.com" ||
		w.Header().Get("Vary") != "Origin" {
		t.Fatalf("allowed origin: %d %v", w.Code, w.Header())
	}
	// Allowed preflight: 204, handler not called.
	before := called
	w = do(http.MethodOptions, "https://ops.example.com", true)
	if w.Code != http.StatusNoContent || called != before || w.Header().Get("Access-Control-Max-Age") != "600" ||
		w.Header().Get("Access-Control-Allow-Methods") == "" {
		t.Fatalf("allowed preflight: %d %v", w.Code, w.Header())
	}
	// Other origin: preflight refused, simple request gets no CORS headers.
	w = do(http.MethodOptions, "https://evil.io", true)
	if w.Code != http.StatusForbidden || w.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("foreign preflight: %d %v", w.Code, w.Header())
	}
	w = do(http.MethodGet, "https://evil.io", false)
	if w.Code != http.StatusTeapot || w.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("foreign GET: %d %v", w.Code, w.Header())
	}
	// A plain OPTIONS (not a preflight) reaches the handler.
	before = called
	if w = do(http.MethodOptions, "https://ops.example.com", false); called != before+1 {
		t.Fatalf("plain OPTIONS swallowed: %d", w.Code)
	}
}
