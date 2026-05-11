package models

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewSnapshot(t *testing.T) {
	s := NewSnapshot("stage", "http://se:8081")
	require.Equal(t, SchemaVersion, s.SchemaVersion)
	require.Equal(t, "paper_trading_snapshot", s.Event)
	require.Equal(t, "stage", s.Environment)
	require.Equal(t, "http://se:8081", s.StrategyExecutorURL)
	require.NotNil(t, s.Indicators)
	require.NotNil(t, s.CollectionErrors)
}
