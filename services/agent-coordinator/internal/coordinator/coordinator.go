package coordinator

import (
	"context"
	"fmt"
	"strings"
	"time"

	"bitso-trading-platform/shared/pkg/agent"
)

type Coordinator struct {
	id          string
	readOnly    bool
	maxFanout   int
	childAgents []ChildAgent
	trace       agent.TraceClient
}

func New(readOnly bool, maxFanout int, trace agent.TraceClient, childAgents []ChildAgent) *Coordinator {
	if trace == nil {
		trace = agent.NoopTraceClient{}
	}
	if maxFanout < 1 {
		maxFanout = 1
	}
	return &Coordinator{
		id:          "agent-coordinator-v1",
		readOnly:    readOnly,
		maxFanout:   maxFanout,
		childAgents: childAgents,
		trace:       trace,
	}
}

func (c *Coordinator) ID() string {
	return c.id
}

func (c *Coordinator) HandleIncident(ctx context.Context, runID string, inc Incident) (CoordinatedReport, error) {
	start := time.Now().UTC()
	runKey, _ := c.trace.StartRun(ctx, agent.TraceRecord{
		RunID:     runID,
		Name:      "coordinator_handle_incident",
		Status:    "started",
		StartedAt: start,
		Metadata:  map[string]string{"incident_title": inc.Title},
	})

	agentReports := make([]AgentRecommendation, 0, len(c.childAgents))
	for i, child := range c.childAgents {
		if i >= c.maxFanout {
			break
		}
		rec, err := child.Triage(ctx, inc)
		if err != nil {
			agentReports = append(agentReports, AgentRecommendation{
				Agent:      child.Name(),
				Summary:    fmt.Sprintf("child agent failed: %v", err),
				Confidence: 0.0,
				Metadata:   map[string]string{"status": "error"},
			})
			continue
		}
		agentReports = append(agentReports, rec)
	}

	report := aggregate(runID, inc, agentReports)
	if c.readOnly {
		report.Recommendations = append(report.Recommendations, "Coordinator is in read-only mode; do not execute automated remediation without operator approval.")
	}

	_ = c.trace.FinishRun(ctx, runKey, agent.TraceRecord{
		RunID:      runID,
		Name:       "coordinator_handle_incident",
		Status:     "completed",
		StartedAt:  start,
		FinishedAt: time.Now().UTC(),
		Metadata: map[string]string{
			"agents_called": fmt.Sprintf("%d", len(agentReports)),
		},
	})

	return report, nil
}

func aggregate(runID string, inc Incident, recs []AgentRecommendation) CoordinatedReport {
	if len(recs) == 0 {
		return CoordinatedReport{
			ID:              runID,
			CreatedAt:       time.Now().UTC(),
			Incident:        inc,
			Summary:         "No child agents available for triage.",
			Confidence:      0,
			AgentReports:    recs,
			Recommendations: []string{"Check coordinator child-agent configuration."},
		}
	}

	total := 0.0
	parts := make([]string, 0, len(recs))
	dedupe := map[string]struct{}{}
	mergedRecs := make([]string, 0, len(recs)*2)
	for _, r := range recs {
		total += r.Confidence
		parts = append(parts, fmt.Sprintf("[%s] %s", r.Agent, r.Summary))
		for _, item := range r.Recommendations {
			key := strings.TrimSpace(strings.ToLower(item))
			if key == "" {
				continue
			}
			if _, ok := dedupe[key]; ok {
				continue
			}
			dedupe[key] = struct{}{}
			mergedRecs = append(mergedRecs, item)
		}
	}

	return CoordinatedReport{
		ID:              runID,
		CreatedAt:       time.Now().UTC(),
		Incident:        inc,
		Summary:         "Coordinated incident triage complete. " + strings.Join(parts, " "),
		Confidence:      total / float64(len(recs)),
		AgentReports:    recs,
		Recommendations: mergedRecs,
	}
}
