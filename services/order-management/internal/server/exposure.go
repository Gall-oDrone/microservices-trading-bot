package server

import (
	"math"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// exposureBookRe accepts Bitso book names such as btc_mxn.
var exposureBookRe = regexp.MustCompile(`^[a-z0-9]{2,10}_[a-z0-9]{2,10}$`)

// ExposureResponse is GET /api/v1/risk/exposure?book=<book> (risk step R5): the
// position and open orders order-management's risk check sees for one book,
// against its position limit, so other services and the operator UI read the
// same numbers the check uses.
type ExposureResponse struct {
	Book            string  `json:"book"`
	PositionSize    float64 `json:"position_size"`  // base currency
	PositionValue   float64 `json:"position_value"` // quote currency, at the position's current price
	OpenOrders      int     `json:"open_orders"`
	MaxPositionSize float64 `json:"max_position_size"` // 0: no limit configured
	// PositionUtilization is |position_size| / max_position_size (0 without a
	// limit); ≥ 1 means a new order that adds to the position is rejected.
	PositionUtilization float64 `json:"position_utilization"`
	AsOf                string  `json:"as_of"`
}

// exposureHandler serves ExposureResponse. Read-only.
func (s *HTTPServer) exposureHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		s.respondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	book := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("book")))
	if !exposureBookRe.MatchString(book) {
		s.respondError(w, http.StatusBadRequest, "book is required, e.g. ?book=btc_mxn")
		return
	}
	exp, err := s.riskManager.GetCurrentExposure(r.Context(), book)
	if err != nil {
		s.respondError(w, http.StatusInternalServerError, "exposure: "+err.Error())
		return
	}
	limits, err := s.riskManager.GetPositionLimits(book)
	if err != nil {
		s.respondError(w, http.StatusInternalServerError, "position limits: "+err.Error())
		return
	}
	resp := ExposureResponse{
		Book:            book,
		PositionSize:    exp.TotalSize,
		PositionValue:   exp.TotalValue,
		OpenOrders:      exp.OpenOrders,
		MaxPositionSize: limits.MaxSize,
		AsOf:            time.Now().UTC().Format(time.RFC3339),
	}
	if limits.MaxSize > 0 {
		resp.PositionUtilization = math.Abs(exp.TotalSize) / limits.MaxSize
	}
	s.respondJSON(w, http.StatusOK, resp)
}
