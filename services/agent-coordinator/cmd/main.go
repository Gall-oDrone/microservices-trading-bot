package main

import (
	"context"
	"log"
	"net"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"bitso-trading-platform/agent-coordinator/internal/config"
	"bitso-trading-platform/agent-coordinator/internal/coordinator"
	"bitso-trading-platform/agent-coordinator/internal/server"
	"bitso-trading-platform/shared/pkg/agent"
)

func main() {
	cfg := config.Load()
	langsmith := agent.LoadLangSmithConfigFromEnv()

	var trace agent.TraceClient = agent.NoopTraceClient{}
	if langsmith.Enabled {
		// Placeholder hook until langsmith client wiring is added.
		trace = agent.NoopTraceClient{}
	}

	children := []coordinator.ChildAgent{
		coordinator.NewHTTPOpsAgentClient(cfg.OpsAgentURL, 10*time.Second),
	}

	coord := coordinator.New(cfg.ReadOnly, cfg.MaxFanoutAgents, trace, children)
	addr := net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))
	srv := server.New(addr, coord)

	go func() {
		log.Printf("agent-coordinator starting on %s (read_only=%t, langsmith_tracing=%t)", addr, cfg.ReadOnly, langsmith.Enabled)
		if err := srv.Start(); err != nil && err.Error() != "http: Server closed" {
			log.Fatalf("agent-coordinator server error: %v", err)
		}
	}()

	waitForSignal()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := srv.Stop(ctx); err != nil {
		log.Printf("shutdown error: %v", err)
	}
}

func waitForSignal() {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	<-ch
}
