package testutils

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestConfig holds configuration for tests
type TestConfig struct {
	Timeout     time.Duration
	RetryCount  int
	RetryDelay  time.Duration
	MockEnabled bool
}

// DefaultTestConfig returns a default test configuration
func DefaultTestConfig() *TestConfig {
	return &TestConfig{
		Timeout:     30 * time.Second,
		RetryCount:  3,
		RetryDelay:  1 * time.Second,
		MockEnabled: true,
	}
}

// WithTimeout sets a custom timeout for the test
func (tc *TestConfig) WithTimeout(timeout time.Duration) *TestConfig {
	tc.Timeout = timeout
	return tc
}

// WithRetries sets custom retry configuration
func (tc *TestConfig) WithRetries(count int, delay time.Duration) *TestConfig {
	tc.RetryCount = count
	tc.RetryDelay = delay
	return tc
}

// AssertEventually asserts that a condition becomes true within the timeout
func AssertEventually(t *testing.T, condition func() bool, timeout time.Duration, msg string) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			assert.Fail(t, msg+": condition not met within timeout")
			return
		case <-ticker.C:
			if condition() {
				return
			}
		}
	}
}

// RequireEventually requires that a condition becomes true within the timeout
func RequireEventually(t *testing.T, condition func() bool, timeout time.Duration, msg string) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			require.Fail(t, msg+": condition not met within timeout")
			return
		case <-ticker.C:
			if condition() {
				return
			}
		}
	}
}
