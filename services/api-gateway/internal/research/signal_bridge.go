package research

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"bitso-trading-platform/shared/pkg/models"
)

// ApproveRequest is the operator gate for emitting a trade signal from a research memo.
type ApproveRequest struct {
	OperatorID string  `json:"operator_id"`
	Book       string  `json:"book,omitempty"`
	Amount     float64 `json:"amount,omitempty"`
	Price      float64 `json:"price,omitempty"`
	Strategy   string  `json:"strategy,omitempty"`
}

// ApproveResult contains audit and Kafka event identifiers.
type ApproveResult struct {
	AuditID       string `json:"audit_id"`
	SignalEventID string `json:"signal_event_id"`
	Signal        string `json:"signal"`
	ResearchRunID string `json:"research_run_id"`
}

// DecisionToSignal maps TradingAgents decision text to BUY/SELL/HOLD.
func DecisionToSignal(decision string) string {
	d := strings.ToUpper(strings.TrimSpace(decision))
	switch {
	case strings.Contains(d, "SELL"):
		return "SELL"
	case strings.Contains(d, "BUY"):
		return "BUY"
	case strings.Contains(d, "HOLD"):
		return "HOLD"
	default:
		return "HOLD"
	}
}

// BuildTradeSignalEvent converts an approved memo into a hot-path TradeSignalEvent.
func BuildTradeSignalEvent(memo map[string]interface{}, req ApproveRequest) (*models.TradeSignalEvent, *ApproveResult, error) {
	runID := stringField(memo, "run_id")
	if runID == "" {
		return nil, nil, fmt.Errorf("memo missing run_id")
	}
	ticker := stringField(memo, "ticker")
	if req.Book != "" {
		ticker = req.Book
	}
	if ticker == "" {
		return nil, nil, fmt.Errorf("book/ticker is required")
	}

	decision := stringField(memo, "decision")
	signal := DecisionToSignal(decision)
	if signal == "HOLD" {
		return nil, nil, fmt.Errorf("memo decision does not map to BUY or SELL: %q", decision)
	}

	operator := strings.TrimSpace(req.OperatorID)
	if operator == "" {
		return nil, nil, fmt.Errorf("operator_id is required for approval")
	}

	auditID := uuid.New().String()
	eventID := fmt.Sprintf("research-%s", auditID)
	strategy := req.Strategy
	if strategy == "" {
		strategy = "research-approved"
	}

	amount := req.Amount
	if amount <= 0 {
		amount = 1
	}

	evt := &models.TradeSignalEvent{
		EventID:   eventID,
		Timestamp: time.Now().UnixMilli(),
		Book:      strings.ToUpper(ticker),
		Strategy:  strategy,
		Signal:    signal,
		Price:     req.Price,
		Amount:    amount,
		Metadata: map[string]interface{}{
			"source":            "research-agent",
			"audit_id":          auditID,
			"research_run_id":   runID,
			"operator_id":       operator,
			"framework":         stringField(memo, "framework"),
			"broker_target":     stringField(memo, "broker_target"),
			"research_decision": decision,
			"trade_date":        stringField(memo, "trade_date"),
		},
	}

	result := &ApproveResult{
		AuditID:       auditID,
		SignalEventID: eventID,
		Signal:        signal,
		ResearchRunID: runID,
	}
	return evt, result, nil
}
