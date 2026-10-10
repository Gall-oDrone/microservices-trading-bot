package config

import (
	"fmt"
	"strings"
)

// Broker identifies which exchange API the trading engine uses.
type Broker string

const (
	BrokerBitso Broker = "bitso"
	BrokerEtoro Broker = "etoro"
)

// ParseBroker normalizes BROKER env (bitso|etoro). Empty defaults to bitso.
func ParseBroker(raw string) (Broker, error) {
	b := Broker(strings.ToLower(strings.TrimSpace(raw)))
	switch b {
	case "", BrokerBitso:
		return BrokerBitso, nil
	case BrokerEtoro:
		return BrokerEtoro, nil
	default:
		return "", fmt.Errorf("invalid BROKER %q: use bitso or etoro", raw)
	}
}

func (b Broker) String() string {
	if b == "" {
		return string(BrokerBitso)
	}
	return string(b)
}

// IsEtoro reports whether the configured broker is eToro.
func (b Broker) IsEtoro() bool {
	return b == BrokerEtoro
}
