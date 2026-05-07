package coordinator

import (
	"context"
	"testing"

	"bitso-trading-platform/shared/pkg/agent"
)

type mockChildAgent struct {
	name string
	rec  AgentRecommendation
	err  error
}

func (m mockChildAgent) Name() string { return m.name }
func (m mockChildAgent) Triage(_ context.Context, _ Incident) (AgentRecommendation, error) {
	return m.rec, m.err
}

type mockTrace struct {
	started int
	ended   int
}

func (m *mockTrace) StartRun(_ context.Context, _ agent.TraceRecord) (string, error) {
	m.started++
	return "run-key", nil
}

func (m *mockTrace) FinishRun(_ context.Context, _ string, _ agent.TraceRecord) error {
	m.ended++
	return nil
}

func TestCoordinator_HandleIncident(t *testing.T) {
	trace := &mockTrace{}
	c := New(true, 3, trace, []ChildAgent{
		mockChildAgent{
			name: "ops-agent",
			rec: AgentRecommendation{
				Agent:           "ops-agent",
				Summary:         "ops triage",
				Confidence:      0.7,
				Recommendations: []string{"Check Prometheus targets"},
			},
		},
		mockChildAgent{
			name: "kafka-agent",
			rec: AgentRecommendation{
				Agent:           "kafka-agent",
				Summary:         "kafka triage",
				Confidence:      0.5,
				Recommendations: []string{"Check Kafka consumer lag"},
			},
		},
	})

	report, err := c.HandleIncident(context.Background(), "r1", Incident{Title: "alert"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(report.AgentReports) != 2 {
		t.Fatalf("expected 2 agent reports, got %d", len(report.AgentReports))
	}
	if report.Confidence <= 0 {
		t.Fatalf("expected confidence > 0, got %f", report.Confidence)
	}
	if len(report.Recommendations) < 2 {
		t.Fatalf("expected recommendations merged from children")
	}
	if trace.started != 1 || trace.ended != 1 {
		t.Fatalf("expected trace start/end once, got start=%d end=%d", trace.started, trace.ended)
	}
}

func TestCoordinator_MaxFanout(t *testing.T) {
	c := New(true, 1, nil, []ChildAgent{
		mockChildAgent{name: "a1", rec: AgentRecommendation{Agent: "a1", Summary: "s", Confidence: 0.6}},
		mockChildAgent{name: "a2", rec: AgentRecommendation{Agent: "a2", Summary: "s", Confidence: 0.6}},
	})
	report, err := c.HandleIncident(context.Background(), "r2", Incident{Title: "fanout"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(report.AgentReports) != 1 {
		t.Fatalf("expected max fanout of 1, got %d", len(report.AgentReports))
	}
}
