package models

// ResearchApprovalRecord is written when an operator approves a cold-path memo for signal emission.
type ResearchApprovalRecord struct {
	AuditID       string `json:"audit_id"`
	ResearchRunID string `json:"research_run_id"`
	OperatorID    string `json:"operator_id"`
	ApprovedAtMs  int64  `json:"approved_at_ms"`
	SignalEventID string `json:"signal_event_id"`
	Ticker        string `json:"ticker"`
	Decision      string `json:"decision"`
	Signal        string `json:"signal"` // BUY, SELL, HOLD
}
