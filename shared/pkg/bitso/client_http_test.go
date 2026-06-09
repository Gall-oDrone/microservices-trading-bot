package bitso

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDoRequest_HTTPErrorNonJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("no healthy upstream"))
	}))
	defer srv.Close()

	c := NewClient()
	c.SetAPIBaseURL(srv.URL + "/")
	c.version = "v3"

	var dest struct {
		Payload Ticker `json:"payload"`
	}
	err := c.getResponse("/ticker", nil, &dest)
	if err == nil {
		t.Fatal("expected error")
	}
	var he *HTTPError
	if !errors.As(err, &he) {
		t.Fatalf("expected *HTTPError, got %T: %v", err, err)
	}
	if he.StatusCode != 503 {
		t.Fatalf("status=%d, want 503", he.StatusCode)
	}
	if he.Body != "no healthy upstream" {
		t.Fatalf("body=%q", he.Body)
	}
}

func TestIsRetryable_HTTPError(t *testing.T) {
	if !IsRetryable(&HTTPError{StatusCode: 503, Body: "no healthy upstream"}) {
		t.Fatal("503 should be retryable")
	}
	if IsRetryable(&HTTPError{StatusCode: 400, Body: "bad request"}) {
		t.Fatal("400 should not be retryable")
	}
}
