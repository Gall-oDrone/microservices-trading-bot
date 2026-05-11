package collector

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"bitso-trading-platform/paper-trading-reporter/internal/models"
)

// HTTPDoer is satisfied by *http.Client.
type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// Collect gathers strategy registry data and per-book indicator snapshots from strategy-executor.
func Collect(ctx context.Context, client HTTPDoer, baseURL string, env string) (*models.PaperTradingSnapshot, error) {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if base == "" {
		return nil, fmt.Errorf("strategy executor base URL is empty")
	}

	snap := models.NewSnapshot(env, base)

	if err := fetchStrategies(ctx, client, base, snap); err != nil {
		snap.CollectionErrors["strategies"] = err.Error()
		return snap, nil
	}

	books := uniqueBooks(snap.Strategies)
	for _, book := range books {
		if book == "" {
			continue
		}
		raw, err := fetchIndicatorSnapshot(ctx, client, base, book)
		if err != nil {
			snap.CollectionErrors["indicators:"+book] = err.Error()
			continue
		}
		snap.Indicators[book] = raw
	}

	return snap, nil
}

func fetchStrategies(ctx context.Context, client HTTPDoer, base string, snap *models.PaperTradingSnapshot) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/v1/strategies", nil)
	if err != nil {
		return err
	}
	resp, err := doWithTimeout(client, req, 30*time.Second)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET /api/v1/strategies: HTTP %d: %s", resp.StatusCode, truncate(string(body), 500))
	}

	var parsed struct {
		Strategies []map[string]interface{} `json:"strategies"`
		Count      int                        `json:"count"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return fmt.Errorf("decode strategies: %w", err)
	}
	snap.Strategies = parsed.Strategies
	if parsed.Count > 0 {
		snap.StrategyCount = parsed.Count
	} else {
		snap.StrategyCount = len(parsed.Strategies)
	}
	return nil
}

func fetchIndicatorSnapshot(ctx context.Context, client HTTPDoer, base, book string) (json.RawMessage, error) {
	u := fmt.Sprintf("%s/api/v1/indicators/%s/snapshot", base, book)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := doWithTimeout(client, req, 15*time.Second)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncate(string(body), 300))
	}
	return json.RawMessage(body), nil
}

func uniqueBooks(strategies []map[string]interface{}) []string {
	seen := make(map[string]struct{})
	var out []string
	for _, s := range strategies {
		b, _ := s["book"].(string)
		if b == "" {
			continue
		}
		if _, ok := seen[b]; ok {
			continue
		}
		seen[b] = struct{}{}
		out = append(out, b)
	}
	return out
}

func doWithTimeout(client HTTPDoer, req *http.Request, d time.Duration) (*http.Response, error) {
	ctx, cancel := context.WithTimeout(req.Context(), d)
	defer cancel()
	req = req.WithContext(ctx)
	return client.Do(req)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
