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

	agentruntime "bitso-trading-platform/ops-agent/internal/agent"
	"bitso-trading-platform/ops-agent/internal/config"
	"bitso-trading-platform/ops-agent/internal/server"
	"bitso-trading-platform/ops-agent/internal/tools"
	"bitso-trading-platform/shared/pkg/agent"
	anthropicprovider "bitso-trading-platform/shared/pkg/agent/providers/anthropic"
	openaiprovider "bitso-trading-platform/shared/pkg/agent/providers/openai"
)

func main() {
	cfg := config.Load()

	policy := agent.NewStaticPolicyEngine(agent.PolicyConfig{
		AllowedTools: map[string]struct{}{
			"http_healthcheck": {},
			"prometheus_query": {},
		},
		Budget: agent.BudgetConfig{
			MaxTotalTokens: cfg.MaxTokens,
			MaxCostUSD:     cfg.MaxCost,
		},
	})

	var provider agent.LLMProvider
	switch cfg.Provider {
	case "openai":
		provider = openaiprovider.NewProvider(cfg.Model)
	default:
		provider = anthropicprovider.NewProvider(cfg.Model)
	}

	opsAgent := agentruntime.NewOpsAgent(provider, policy, []agent.Tool{
		tools.NewHTTPHealthCheckTool(config.DefaultHTTPTimeout()),
		tools.NewPrometheusQueryTool(cfg.PrometheusBaseURL, config.DefaultHTTPTimeout()),
	})

	addr := net.JoinHostPort(cfg.Host, intToString(cfg.Port))
	srv := server.New(addr, opsAgent)

	go func() {
		log.Printf("ops-agent starting on %s (provider=%s read_only=%t langchain_enabled=%t)", addr, cfg.Provider, cfg.ReadOnly, cfg.EnableLangChain)
		if err := srv.Start(); err != nil && err.Error() != "http: Server closed" {
			log.Fatalf("ops-agent server error: %v", err)
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

func intToString(v int) string {
	return strconv.Itoa(v)
}
