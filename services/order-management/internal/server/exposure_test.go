package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"bitso-trading-platform/order-management/internal/models"
	"bitso-trading-platform/order-management/internal/risk"
)

type fakeRisk struct {
	exp    risk.Exposure
	max    float64
	err    error
	booked string
}

func (f *fakeRisk) CheckRisk(context.Context, *models.Order) error           { return nil }
func (f *fakeRisk) CheckPositionLimits(context.Context, *models.Order) error { return nil }
func (f *fakeRisk) CheckOrderLimits(context.Context, *models.Order) error    { return nil }
func (f *fakeRisk) GetPositionLimits(string) (*risk.PositionLimits, error) {
	return &risk.PositionLimits{MaxSize: f.max}, nil
}
func (f *fakeRisk) GetCurrentExposure(_ context.Context, book string) (*risk.Exposure, error) {
	f.booked = book
	if f.err != nil {
		return nil, f.err
	}
	e := f.exp
	return &e, nil
}

func TestExposureHandler(t *testing.T) {
	f := &fakeRisk{exp: risk.Exposure{TotalSize: -0.005, TotalValue: -6000, OpenOrders: 2}, max: 0.01}
	s := &HTTPServer{riskManager: f}

	rec := httptest.NewRecorder()
	s.exposureHandler(rec, httptest.NewRequest(http.MethodGet, "/api/v1/risk/exposure?book=BTC_MXN", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	var got ExposureResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Book != "btc_mxn" || f.booked != "btc_mxn" || got.OpenOrders != 2 || got.MaxPositionSize != 0.01 ||
		got.PositionUtilization != 0.5 || got.PositionSize != -0.005 || got.AsOf == "" {
		t.Fatalf("response %+v", got)
	}

	for _, tc := range []struct {
		method, url string
		want        int
	}{
		{http.MethodGet, "/api/v1/risk/exposure", http.StatusBadRequest},
		{http.MethodGet, "/api/v1/risk/exposure?book=../etc", http.StatusBadRequest},
		{http.MethodPost, "/api/v1/risk/exposure?book=btc_mxn", http.StatusMethodNotAllowed},
	} {
		rec := httptest.NewRecorder()
		s.exposureHandler(rec, httptest.NewRequest(tc.method, tc.url, nil))
		if rec.Code != tc.want {
			t.Errorf("%s %s = %d, want %d", tc.method, tc.url, rec.Code, tc.want)
		}
	}

	// No limit configured: utilization 0, not +Inf.
	f.max = 0
	rec = httptest.NewRecorder()
	s.exposureHandler(rec, httptest.NewRequest(http.MethodGet, "/api/v1/risk/exposure?book=btc_usd", nil))
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || got.PositionUtilization != 0 {
		t.Fatalf("no limit: %+v %v", got, err)
	}

	f.err = errors.New("redis down")
	rec = httptest.NewRecorder()
	s.exposureHandler(rec, httptest.NewRequest(http.MethodGet, "/api/v1/risk/exposure?book=btc_mxn", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("repo error = %d", rec.Code)
	}
}
