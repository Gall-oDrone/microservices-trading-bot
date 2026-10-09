package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSample_ParsesTheRESTBook(t *testing.T) {
	var gotURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURL = r.URL.String()
		w.Write([]byte(`{"success":true,"payload":{"bids":[{"price":"99.99","amount":"0.5"},{"price":"99.9","amount":"1"}],` +
			`"asks":[{"price":"100.01","amount":"0.5"},{"price":"100.1","amount":"1"}]}}`))
	}))
	defer srv.Close()
	now := time.Date(2026, 10, 9, 21, 0, 0, 0, time.UTC)
	s, err := sample(srv.Client(), srv.URL, "btc_mxn", 10, now)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(gotURL, "/v3/order_book/") || !strings.Contains(gotURL, "book=btc_mxn") || !strings.Contains(gotURL, "aggregate=true") {
		t.Fatalf("url %s", gotURL)
	}
	if s.Mid != 100 || s.BidDepthBTC != 1.5 || s.Source != "bitso-rest" || s.At != "2026-10-09T21:00:00Z" || s.BudgetBps != 10 {
		t.Fatalf("sample %+v", s)
	}
}

func TestSample_Errors(t *testing.T) {
	for name, body := range map[string]string{
		"not success": `{"success":false}`,
		"bad level":   `{"success":true,"payload":{"bids":[{"price":"x","amount":"1"}],"asks":[]}}`,
		"crossed":     `{"success":true,"payload":{"bids":[{"price":"101","amount":"1"}],"asks":[{"price":"100","amount":"1"}]}}`,
		"garbage":     `<html>`,
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(body)) }))
		if _, err := sample(srv.Client(), srv.URL, "btc_mxn", 10, time.Now()); err == nil {
			t.Errorf("%s: want an error", name)
		}
		srv.Close()
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusTooManyRequests) }))
	defer srv.Close()
	if _, err := sample(srv.Client(), srv.URL, "btc_mxn", 10, time.Now()); err == nil || !strings.Contains(err.Error(), "429") {
		t.Fatalf("want HTTP 429, got %v", err)
	}
}
