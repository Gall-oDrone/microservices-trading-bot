package metrics

import (
	"testing"
	"time"
)

// Single test that creates one Collector and exercises all Record* methods.
// promauto registers in the default registry, so multiple NewCollector() in the same process would panic (duplicate registration).
func TestCollector_RecordMethods_NoPanic(t *testing.T) {
	c := NewCollector()
	if c == nil {
		t.Fatal("NewCollector returned nil")
	}

	// Metrics used by dashboards and alerts must be recordable without panic.
	c.RecordOrderExecuted("btc_mxn", "intraday")
	c.RecordOrderFailed("btc_mxn", "intraday", ReasonValidation)
	c.ObserveOrderExecutionDuration("btc_mxn", "intraday", 100*time.Millisecond)

	c.RecordSignalReceived()
	c.RecordSignalsProcessed("btc_mxn", "intraday", OutcomeSuccess)
	c.RecordSignalsDropped(DropReasonChannelFull)
	c.ObserveSignalProcessingDuration(50 * time.Millisecond)

	c.RecordSessionRiskCheck(SessionRiskAllowed)
	c.RecordSessionRiskRejection()
	c.RecordSessionRiskCheck(SessionRiskRejected)

	c.RecordKafkaMessageConsumed("signals")
	c.RecordKafkaConsumerError()
	c.RecordOrderPlacedPublished()
	c.RecordOrderPlacedPublishError()

	c.RecordBalances(map[string]float64{"MXN": 1000})
	c.RecordBalanceFetchError()
	c.SetBalanceLastSuccessTimestamp(float64(time.Now().Unix()))
	c.SetEngineState(2)
	c.SetDryRun(true)
	c.RecordHealthCheckFailure()

	h := c.Handler()
	if h == nil {
		t.Fatal("Handler returned nil")
	}
}
