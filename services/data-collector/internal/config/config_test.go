package config_test

import (
	"reflect"
	"testing"

	"bitso-trading-platform/data-collector/internal/config"
)

func TestParseBooksDefaultPair(t *testing.T) {
	got, err := config.ParseBooks(config.DefaultBitsoBooks)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"btc_mxn", "btc_usd"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestParseBooksTrimsDedupesAndLowercases(t *testing.T) {
	got, err := config.ParseBooks(" BTC_MXN, btc_usd,btc_mxn, ")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"btc_mxn", "btc_usd"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestParseBooksRejectsInvalid(t *testing.T) {
	if _, err := config.ParseBooks(""); err == nil {
		t.Fatal("expected error for empty")
	}
	if _, err := config.ParseBooks("btc"); err == nil {
		t.Fatal("expected error for missing minor")
	}
	if _, err := config.ParseBooks("btc_"); err == nil {
		t.Fatal("expected error for empty minor")
	}
}
