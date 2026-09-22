// Package loader builds backtest inputs from archived market data.
//
// The canonical source is the S3 Parquet trade archive written by
// services/data-collector (see the feat/intraday-data-collector branch,
// internal/sink/batcher.go). The archive is the ONLY source able to serve the
// full history: the Postgres hot store is pruned to a rolling retention window
// (~7 days) by PostgresStore.WriteTrades, so it cannot answer questions about
// anything older than that.
//
// Layout written by the collector:
//
//	trades/book=<book>/year=YYYY/month=MM/day=DD/trades-<flushTS>.parquet
//
// Two properties of that layout drive this package's design:
//
//  1. There is NO fixed number of files per day. The collector flushes on a row
//     threshold or a timer, so a day may hold thousands of small objects. A
//     compaction effort is in flight on the collector branch; this loader is
//     agnostic to it because it enumerates every object under the day prefix
//     rather than assuming a filename or a count.
//
//  2. Rows can repeat. The collector re-queues a failed batch and retries the
//     flush, and the Postgres sink deduplicates with ON CONFLICT (book, tid) DO
//     NOTHING. S3 has no such guard, so duplicate (book, tid) rows can and do
//     land in the archive. Normalize deduplicates on that same key.
//
// Everything in this package is driven through narrow interfaces (ObjectStore,
// RowScanner) so the unit tests run against in-memory fixtures with no AWS or
// Postgres connection.
package loader

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"bitso-trading-platform/strategy-executor/internal/backtest"
	"bitso-trading-platform/strategy-executor/internal/indicators"
)

// ArchiveTrade is the decoded form of one archived row, before it is narrowed
// to the indicators.Trade shape the backtest engine consumes. It is kept
// separate because the archive carries fields (TID, ReceivedAt, MakerSide) that
// indicators.Trade has nowhere to put but that this package needs for
// deduplication and diagnostics.
type ArchiveTrade struct {
	Book       string
	TID        int64
	Price      float64
	Amount     float64
	MakerSide  string
	ExchangeTS time.Time
	ReceivedAt time.Time
}

// TakerSide returns the side of the aggressor. Bitso reports maker_side — the
// side that was resting on the book — so the taker is its opposite. This
// mirrors models.TradeEvent.GetTakerSide in services/market-data.
//
// Note that no indicator in services/strategy-executor/internal/indicators
// reads Trade.Side; it is carried for diagnostics and for strategies that may
// later want order-flow context. The mapping is made explicit here so that a
// future consumer does not have to guess the convention.
func (a ArchiveTrade) TakerSide() string {
	switch strings.ToLower(strings.TrimSpace(a.MakerSide)) {
	case "buy":
		return "sell"
	case "sell":
		return "buy"
	default:
		return ""
	}
}

// ToIndicatorTrade narrows an archived row to the engine's trade shape.
//
// ExchangeTS (when the exchange matched the trade) is used rather than
// ReceivedAt (when our collector saw it). Using ReceivedAt would fold collector
// latency and reconnect backlogs into the bar timestamps, which would distort
// every time-bucketed indicator downstream.
func (a ArchiveTrade) ToIndicatorTrade() indicators.Trade {
	return indicators.Trade{
		Timestamp: a.ExchangeTS,
		Price:     a.Price,
		Amount:    a.Amount,
		Side:      a.TakerSide(),
	}
}

// Stats summarizes what a load actually produced. Callers should report these
// numbers rather than assuming the load was clean: DuplicatesDropped and
// OutOfRangeDropped in particular are how a silently truncated or
// double-written archive becomes visible.
type Stats struct {
	Source           string
	Book             string
	From             time.Time
	To               time.Time
	ObjectsListed    int
	ObjectsRead      int
	ObjectsFailed    int
	RowsDecoded      int
	DuplicatesResult int
	OutOfRange       int
	Invalid          int
	TradesReturned   int
	FirstTrade       time.Time
	LastTrade        time.Time
}

// String renders the stats as a single human-readable line.
func (s Stats) String() string {
	return fmt.Sprintf(
		"source=%s book=%s window=[%s..%s] objects=%d/%d (failed=%d) rows=%d dup=%d out_of_range=%d invalid=%d returned=%d span=[%s..%s]",
		s.Source, s.Book,
		s.From.UTC().Format(time.RFC3339), s.To.UTC().Format(time.RFC3339),
		s.ObjectsRead, s.ObjectsListed, s.ObjectsFailed,
		s.RowsDecoded, s.DuplicatesResult, s.OutOfRange, s.Invalid, s.TradesReturned,
		formatTS(s.FirstTrade), formatTS(s.LastTrade),
	)
}

func formatTS(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.UTC().Format(time.RFC3339)
}

// TradeSource loads archived trades for a book over a half-open-ish window.
// Implementations are inclusive of both bounds; callers that need exclusivity
// should trim the result.
type TradeSource interface {
	// LoadTrades returns archived rows ordered by exchange timestamp.
	LoadTrades(ctx context.Context, book string, from, to time.Time) ([]ArchiveTrade, Stats, error)
	// Describe names the source for reporting (e.g. "s3://bucket/trades").
	Describe() string
}

// Normalize deduplicates, range-filters, validates and sorts archived rows.
//
// Order matters: validation runs before deduplication so that a malformed
// duplicate cannot mask a well-formed one.
func Normalize(rows []ArchiveTrade, book string, from, to time.Time, st *Stats) []ArchiveTrade {
	seen := make(map[int64]struct{}, len(rows))
	out := make([]ArchiveTrade, 0, len(rows))

	for _, r := range rows {
		if book != "" && r.Book != "" && r.Book != book {
			st.Invalid++
			continue
		}
		// A zero or negative price/amount is not a trade; it is a decode or
		// upstream error. Dropping silently would quietly bias VWAP and the
		// bar Low, so it is counted.
		if r.Price <= 0 || r.Amount <= 0 || r.ExchangeTS.IsZero() {
			st.Invalid++
			continue
		}
		if !from.IsZero() && r.ExchangeTS.Before(from) {
			st.OutOfRange++
			continue
		}
		if !to.IsZero() && r.ExchangeTS.After(to) {
			st.OutOfRange++
			continue
		}
		if _, dup := seen[r.TID]; dup {
			st.DuplicatesResult++
			continue
		}
		seen[r.TID] = struct{}{}
		out = append(out, r)
	}

	// Stable sort on (ExchangeTS, TID). TID breaks ties so that two trades
	// stamped in the same millisecond replay in exchange sequence order rather
	// than in whatever order S3 happened to list the objects.
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].ExchangeTS.Equal(out[j].ExchangeTS) {
			return out[i].TID < out[j].TID
		}
		return out[i].ExchangeTS.Before(out[j].ExchangeTS)
	})

	st.TradesReturned = len(out)
	if len(out) > 0 {
		st.FirstTrade = out[0].ExchangeTS
		st.LastTrade = out[len(out)-1].ExchangeTS
	}
	return out
}

// ToIndicatorTrades converts archived rows to the engine's trade slice.
func ToIndicatorTrades(rows []ArchiveTrade) []indicators.Trade {
	out := make([]indicators.Trade, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.ToIndicatorTrade())
	}
	return out
}

// LoadProvider loads a window from src and wraps it in the engine's provider.
//
// This is the single call the backtest harness needs; it is the seam the task
// description refers to as "feeds them into backtest.NewBacktestDataProvider".
func LoadProvider(ctx context.Context, src TradeSource, book string, from, to time.Time) (*backtest.BacktestDataProvider, Stats, error) {
	rows, st, err := src.LoadTrades(ctx, book, from, to)
	if err != nil {
		return nil, st, err
	}
	return backtest.NewBacktestDataProvider(ToIndicatorTrades(rows)), st, nil
}

// LoadTradesOnly loads a window and returns the raw engine trade slice, for
// callers (such as backtest.RunHistorical) that take []indicators.Trade
// directly rather than a provider.
func LoadTradesOnly(ctx context.Context, src TradeSource, book string, from, to time.Time) ([]indicators.Trade, Stats, error) {
	rows, st, err := src.LoadTrades(ctx, book, from, to)
	if err != nil {
		return nil, st, err
	}
	return ToIndicatorTrades(rows), st, nil
}
