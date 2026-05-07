package coordinator

import "time"

type Incident struct {
	Source      string            `json:"source"`
	Severity    string            `json:"severity"`
	Title       string            `json:"title"`
	Description string            `json:"description"`
	Labels      map[string]string `json:"labels"`
}

type AgentRecommendation struct {
	Agent           string            `json:"agent"`
	Summary         string            `json:"summary"`
	Confidence      float64           `json:"confidence"`
	Recommendations []string          `json:"recommendations"`
	Metadata        map[string]string `json:"metadata,omitempty"`
}

type CoordinatedReport struct {
	ID              string                `json:"id"`
	CreatedAt       time.Time             `json:"created_at"`
	Incident        Incident              `json:"incident"`
	Summary         string                `json:"summary"`
	Confidence      float64               `json:"confidence"`
	AgentReports    []AgentRecommendation `json:"agent_reports"`
	Recommendations []string              `json:"recommendations"`
}
