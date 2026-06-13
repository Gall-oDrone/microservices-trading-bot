package processor

import (
	"testing"
	"time"
)

func TestShouldWarnTradeSilence(t *testing.T) {
	threshold := 5 * time.Minute
	cooldown := 2 * time.Minute
	now := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name        string
		lastTrade   time.Time
		lastWarning time.Time
		want        bool
	}{
		{
			name:      "no trades yet",
			lastTrade: time.Time{},
			want:      false,
		},
		{
			name:      "recent trade",
			lastTrade: now.Add(-2 * time.Minute),
			want:      false,
		},
		{
			name:      "stale trade",
			lastTrade: now.Add(-10 * time.Minute),
			want:      true,
		},
		{
			name:        "stale but within warn cooldown",
			lastTrade:   now.Add(-10 * time.Minute),
			lastWarning: now.Add(-1 * time.Minute),
			want:        false,
		},
		{
			name:        "stale and warn cooldown elapsed",
			lastTrade:   now.Add(-10 * time.Minute),
			lastWarning: now.Add(-3 * time.Minute),
			want:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldWarnTradeSilence(tt.lastTrade, threshold, cooldown, tt.lastWarning, now)
			if got != tt.want {
				t.Fatalf("shouldWarnTradeSilence() = %v, want %v", got, tt.want)
			}
		})
	}
}
