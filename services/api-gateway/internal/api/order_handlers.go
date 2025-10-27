package api

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"bitso-trading-platform/api-gateway/internal/client"
	"bitso-trading-platform/api-gateway/internal/logger"
	"bitso-trading-platform/api-gateway/internal/metrics"
)

// OrderHandler handles order management related requests
type OrderHandler struct {
	client  client.OrderManagementClient
	logger  *logger.Logger
	metrics *metrics.MetricsCollector
}

// NewOrderHandler creates a new order handler
func NewOrderHandler(
	client client.OrderManagementClient,
	logger *logger.Logger,
	metrics *metrics.MetricsCollector,
) *OrderHandler {
	return &OrderHandler{
		client:  client,
		logger:  logger.WithComponent("order-handler"),
		metrics: metrics,
	}
}

// HandleListOrders handles GET /api/v1/orders
func (h *OrderHandler) HandleListOrders(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		MethodNotAllowedResponse(w, r)
		return
	}

	// Parse query parameters
	filters := h.parseOrderFilters(r)

	h.logger.Debug("Listing orders", map[string]interface{}{
		"filters": filters,
	})

	// Call client
	orderList, err := h.client.ListOrders(r.Context(), filters)
	if err != nil {
		h.logger.Error("Failed to list orders", map[string]interface{}{
			"error": err.Error(),
		})
		InternalErrorResponse(w, r, err)
		return
	}

	SuccessResponse(w, r, orderList)
}

// HandleGetOrder handles GET /api/v1/orders/{id}
func (h *OrderHandler) HandleGetOrder(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		MethodNotAllowedResponse(w, r)
		return
	}

	// Extract order ID from path
	orderID := strings.TrimPrefix(r.URL.Path, "/api/v1/orders/")
	if orderID == "" {
		BadRequestResponse(w, r, "Order ID is required")
		return
	}

	h.logger.Debug("Getting order", map[string]interface{}{
		"order_id": orderID,
	})

	// Call client
	order, err := h.client.GetOrder(r.Context(), orderID)
	if err != nil {
		h.logger.Error("Failed to get order", map[string]interface{}{
			"error":    err.Error(),
			"order_id": orderID,
		})
		InternalErrorResponse(w, r, err)
		return
	}

	SuccessResponse(w, r, order)
}

// HandleCancelOrder handles POST /api/v1/orders/{id}/cancel
func (h *OrderHandler) HandleCancelOrder(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		MethodNotAllowedResponse(w, r)
		return
	}

	// Extract order ID from path
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/orders/")
	orderID := strings.TrimSuffix(path, "/cancel")
	
	if orderID == "" || orderID == path {
		BadRequestResponse(w, r, "Order ID is required")
		return
	}

	h.logger.Info("Cancelling order", map[string]interface{}{
		"order_id": orderID,
	})

	// Call client
	err := h.client.CancelOrder(r.Context(), orderID)
	if err != nil {
		h.logger.Error("Failed to cancel order", map[string]interface{}{
			"error":    err.Error(),
			"order_id": orderID,
		})
		InternalErrorResponse(w, r, err)
		return
	}

	SuccessResponse(w, r, map[string]interface{}{
		"message":  "Order cancelled successfully",
		"order_id": orderID,
	})
}

// HandleGetActiveOrders handles GET /api/v1/orders/active
func (h *OrderHandler) HandleGetActiveOrders(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		MethodNotAllowedResponse(w, r)
		return
	}

	h.logger.Debug("Getting active orders", nil)

	// Call client
	orders, err := h.client.GetActiveOrders(r.Context())
	if err != nil {
		h.logger.Error("Failed to get active orders", map[string]interface{}{
			"error": err.Error(),
		})
		InternalErrorResponse(w, r, err)
		return
	}

	SuccessResponse(w, r, map[string]interface{}{
		"orders": orders,
		"total":  len(orders),
	})
}

// HandleGetOrderHistory handles GET /api/v1/orders/history
func (h *OrderHandler) HandleGetOrderHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		MethodNotAllowedResponse(w, r)
		return
	}

	// Parse query parameters
	filters := h.parseOrderFilters(r)

	h.logger.Debug("Getting order history", map[string]interface{}{
		"filters": filters,
	})

	// Call client
	orders, err := h.client.GetOrderHistory(r.Context(), filters)
	if err != nil {
		h.logger.Error("Failed to get order history", map[string]interface{}{
			"error": err.Error(),
		})
		InternalErrorResponse(w, r, err)
		return
	}

	SuccessResponse(w, r, map[string]interface{}{
		"orders": orders,
		"total":  len(orders),
	})
}

// HandleListPositions handles GET /api/v1/positions
func (h *OrderHandler) HandleListPositions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		MethodNotAllowedResponse(w, r)
		return
	}

	// Parse query parameters
	filters := h.parsePositionFilters(r)

	h.logger.Debug("Listing positions", map[string]interface{}{
		"filters": filters,
	})

	// Call client
	positions, err := h.client.ListPositions(r.Context(), filters)
	if err != nil {
		h.logger.Error("Failed to list positions", map[string]interface{}{
			"error": err.Error(),
		})
		InternalErrorResponse(w, r, err)
		return
	}

	SuccessResponse(w, r, map[string]interface{}{
		"positions": positions,
		"total":     len(positions),
	})
}

// HandleGetPosition handles GET /api/v1/positions/{book}
func (h *OrderHandler) HandleGetPosition(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		MethodNotAllowedResponse(w, r)
		return
	}

	// Extract book from path
	book := strings.TrimPrefix(r.URL.Path, "/api/v1/positions/")
	if book == "" {
		BadRequestResponse(w, r, "Book is required")
		return
	}

	h.logger.Debug("Getting position", map[string]interface{}{
		"book": book,
	})

	// Call client
	position, err := h.client.GetPosition(r.Context(), book)
	if err != nil {
		h.logger.Error("Failed to get position", map[string]interface{}{
			"error": err.Error(),
			"book":  book,
		})
		InternalErrorResponse(w, r, err)
		return
	}

	SuccessResponse(w, r, position)
}

// HandleGetPositionSummary handles GET /api/v1/positions/summary
func (h *OrderHandler) HandleGetPositionSummary(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		MethodNotAllowedResponse(w, r)
		return
	}

	h.logger.Debug("Getting position summary", nil)

	// Call client
	summary, err := h.client.GetPositionSummary(r.Context())
	if err != nil {
		h.logger.Error("Failed to get position summary", map[string]interface{}{
			"error": err.Error(),
		})
		InternalErrorResponse(w, r, err)
		return
	}

	SuccessResponse(w, r, summary)
}

// parseOrderFilters parses order filters from query parameters
func (h *OrderHandler) parseOrderFilters(r *http.Request) *client.OrderFilters {
	query := r.URL.Query()
	
	filters := &client.OrderFilters{
		Book:      query.Get("book"),
		Status:    query.Get("status"),
		Strategy:  query.Get("strategy"),
		Side:      query.Get("side"),
		SortBy:    query.Get("sort_by"),
		SortOrder: query.Get("sort_order"),
	}

	// Parse limit
	if limitStr := query.Get("limit"); limitStr != "" {
		if limit, err := strconv.Atoi(limitStr); err == nil && limit > 0 {
			filters.Limit = limit
		}
	}

	// Parse offset
	if offsetStr := query.Get("offset"); offsetStr != "" {
		if offset, err := strconv.Atoi(offsetStr); err == nil && offset >= 0 {
			filters.Offset = offset
		}
	}

	// Parse time range
	if fromStr := query.Get("from"); fromStr != "" {
		if from, err := time.Parse(time.RFC3339, fromStr); err == nil {
			filters.From = &from
		}
	}

	if toStr := query.Get("to"); toStr != "" {
		if to, err := time.Parse(time.RFC3339, toStr); err == nil {
			filters.To = &to
		}
	}

	return filters
}

// parsePositionFilters parses position filters from query parameters
func (h *OrderHandler) parsePositionFilters(r *http.Request) *client.PositionFilters {
	query := r.URL.Query()
	
	filters := &client.PositionFilters{
		Book:   query.Get("book"),
		Status: query.Get("status"),
	}

	// Parse limit
	if limitStr := query.Get("limit"); limitStr != "" {
		if limit, err := strconv.Atoi(limitStr); err == nil && limit > 0 {
			filters.Limit = limit
		}
	}

	// Parse offset
	if offsetStr := query.Get("offset"); offsetStr != "" {
		if offset, err := strconv.Atoi(offsetStr); err == nil && offset >= 0 {
			filters.Offset = offset
		}
	}

	return filters
}

