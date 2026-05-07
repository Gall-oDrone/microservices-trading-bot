package kafka

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	kafka "github.com/segmentio/kafka-go"
)

// TopicSpec describes a Kafka topic that a service requires at startup.
// Pre-creating topics avoids a known race where a kafka-go consumer joins a
// consumer group before the topic exists: the leader assigns 0 partitions and
// never rebalances, so the consumer is silently stuck at empty assignments
// even after the topic appears (auto.create.topics.enable=true).
type TopicSpec struct {
	Name              string
	NumPartitions     int
	ReplicationFactor int
}

// EnsureTopics best-effort-creates the listed topics on the cluster reachable
// via brokers. It is idempotent: existing topics are silently kept (broker
// returns "topic already exists" which is treated as success).
//
// EnsureTopics should be called early in service startup, before any consumer
// or producer is created, so that subsequent group joins receive partition
// assignments. Errors are returned but callers may choose to log and continue
// (auto-create still applies as a fallback).
func EnsureTopics(ctx context.Context, brokers []string, topics []TopicSpec) error {
	if len(brokers) == 0 {
		return errors.New("no brokers")
	}
	if len(topics) == 0 {
		return nil
	}
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	var lastErr error
	for _, b := range brokers {
		b = strings.TrimSpace(b)
		if b == "" {
			continue
		}
		dctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		conn, err := kafka.DialContext(dctx, "tcp", b)
		cancel()
		if err != nil {
			lastErr = fmt.Errorf("dial %s: %w", b, err)
			continue
		}
		controller, err := conn.Controller()
		_ = conn.Close()
		if err != nil {
			lastErr = fmt.Errorf("controller from %s: %w", b, err)
			continue
		}
		caddr := net.JoinHostPort(controller.Host, strconv.Itoa(controller.Port))
		dctx, cancel = context.WithTimeout(ctx, 5*time.Second)
		cc, err := dialer.DialContext(dctx, "tcp", caddr)
		cancel()
		if err != nil {
			lastErr = fmt.Errorf("dial controller %s: %w", caddr, err)
			continue
		}
		ctrlConn := kafka.NewConn(cc, "", 0)
		var configs []kafka.TopicConfig
		for _, t := range topics {
			if strings.TrimSpace(t.Name) == "" {
				continue
			}
			n := t.NumPartitions
			if n <= 0 {
				n = 1
			}
			rf := t.ReplicationFactor
			if rf <= 0 {
				rf = 1
			}
			configs = append(configs, kafka.TopicConfig{
				Topic:             t.Name,
				NumPartitions:     n,
				ReplicationFactor: rf,
			})
		}
		err = ctrlConn.CreateTopics(configs...)
		_ = ctrlConn.Close()
		if err == nil {
			return nil
		}
		// "Topic with this name already exists" is fine.
		if strings.Contains(strings.ToLower(err.Error()), "already exists") {
			return nil
		}
		lastErr = err
	}
	return lastErr
}
