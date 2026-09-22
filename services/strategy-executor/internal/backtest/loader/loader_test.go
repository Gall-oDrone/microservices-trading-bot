package loader

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/xitongsys/parquet-go/parquet"
	"github.com/xitongsys/parquet-go/writer"
)

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

// writeFixtureParquet encodes rows using the SAME writer configuration the
// collector uses (parquet-go, SNAPPY, identical struct tags). If the collector
// schema ever drifts, this fixture keeps compiling but the decode assertions
// below will catch the mismatch.
func writeFixtureParquet(t *testing.T, rows []parquetTrade) []byte {
	t.Helper()

	buf := new(bytes.Buffer)
	pw, err := writer.NewParquetWriterFromWriter(buf, new(parquetTrade), 4)
	if err != nil {
		t.Fatalf("new parquet writer: %v", err)
	}
	pw.CompressionType = parquet.CompressionCodec_SNAPPY
	for _, r := range rows {
		if err := pw.Write(r); err != nil {
			t.Fatalf("write row: %v", err)
		}
	}
	if err := pw.WriteStop(); err != nil {
		t.Fatalf("write stop: %v", err)
	}
	return buf.Bytes()
}

func ms(t time.Time) int64 { return t.UTC().UnixMilli() }

// memStore is an in-memory ObjectStore. No AWS, no network.
type memStore struct {
	objects map[string][]byte
	listErr error
	getErr  map[string]error
	// listCalls records the prefixes requested, so tests can assert the
	// day-partition enumeration is correct.
	listCalls []string
}

func newMemStore() *memStore {
	return &memStore{objects: map[string][]byte{}, getErr: map[string]error{}}
}

func (m *memStore) ListObjects(_ context.Context, prefix string) ([]string, error) {
	m.listCalls = append(m.listCalls, prefix)
	if m.listErr != nil {
		return nil, m.listErr
	}
	var out []string
	for k := range m.objects {
		if strings.HasPrefix(k, prefix) {
			out = append(out, k)
		}
	}
	return out, nil
}

func (m *memStore) GetObject(_ context.Context, key string) ([]byte, error) {
	if err, ok := m.getErr[key]; ok {
		return nil, err
	}
	b, ok := m.objects[key]
	if !ok {
		return nil, fmt.Errorf("no such key %s", key)
	}
	return b, nil
}

// ---------------------------------------------------------------------------
// Parquet decoding
// ---------------------------------------------------------------------------

func TestDecodeParquetTrades_RoundTripsCollectorSchema(t *testing.T) {
	base := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	fixture := []parquetTrade{
		{Book: "btc_mxn", TID: 1, Price: 1_900_000, Amount: 0.01, MakerSide: "buy", ExchangeTS: ms(base), ReceivedAt: ms(base.Add(80 * time.Millisecond))},
		{Book: "btc_mxn", TID: 2, Price: 1_900_500, Amount: 0.02, MakerSide: "sell", ExchangeTS: ms(base.Add(time.Second)), ReceivedAt: ms(base.Add(1100 * time.Millisecond))},
	}

	got, err := DecodeParquetTrades(writeFixtureParquet(t, fixture))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d rows, want 2", len(got))
	}

	if got[0].Book != "btc_mxn" || got[0].TID != 1 || got[0].Price != 1_900_000 || got[0].Amount != 0.01 {
		t.Errorf("row 0 mismatch: %+v", got[0])
	}
	// TIMESTAMP_MILLIS must decode back to the exact UTC instant.
	if !got[0].ExchangeTS.Equal(base) {
		t.Errorf("exchange_ts = %s, want %s", got[0].ExchangeTS, base)
	}
	if !got[1].ReceivedAt.Equal(base.Add(1100 * time.Millisecond)) {
		t.Errorf("received_at = %s, want %s", got[1].ReceivedAt, base.Add(1100*time.Millisecond))
	}
}

func TestDecodeParquetTrades_EmptyPayloadIsAnError(t *testing.T) {
	if _, err := DecodeParquetTrades(nil); err == nil {
		t.Fatal("expected an error for an empty payload, got nil")
	}
}

func TestDecodeParquetTrades_GarbageIsAnError(t *testing.T) {
	if _, err := DecodeParquetTrades([]byte("this is not parquet")); err == nil {
		t.Fatal("expected an error for a non-parquet payload, got nil")
	}
}

// ---------------------------------------------------------------------------
// Side mapping
// ---------------------------------------------------------------------------

func TestArchiveTrade_TakerSideIsOppositeOfMakerSide(t *testing.T) {
	cases := []struct {
		maker string
		want  string
	}{
		{"buy", "sell"},
		{"sell", "buy"},
		{"BUY", "sell"},
		{" sell ", "buy"},
		{"", ""},
		{"unknown", ""},
	}
	for _, c := range cases {
		t.Run("maker="+c.maker, func(t *testing.T) {
			if got := (ArchiveTrade{MakerSide: c.maker}).TakerSide(); got != c.want {
				t.Errorf("TakerSide() = %q, want %q", got, c.want)
			}
		})
	}
}

func TestArchiveTrade_UsesExchangeTimestampNotReceivedAt(t *testing.T) {
	exch := time.Date(2026, 8, 21, 10, 0, 0, 0, time.UTC)
	recv := exch.Add(45 * time.Second) // a reconnect backlog replaying late

	got := ArchiveTrade{Price: 1, Amount: 1, ExchangeTS: exch, ReceivedAt: recv}.ToIndicatorTrade()
	if !got.Timestamp.Equal(exch) {
		t.Fatalf("Timestamp = %s, want exchange_ts %s (received_at would fold collector latency into every bar)", got.Timestamp, exch)
	}
}

// ---------------------------------------------------------------------------
// Normalization
// ---------------------------------------------------------------------------

func TestNormalize(t *testing.T) {
	base := time.Date(2026, 8, 21, 0, 0, 0, 0, time.UTC)
	from := base
	to := base.Add(time.Hour)

	tests := []struct {
		name     string
		rows     []ArchiveTrade
		wantTIDs []int64
		wantDup  int
		wantOOR  int
		wantBad  int
	}{
		{
			name: "drops duplicate tids (S3 has no ON CONFLICT guard)",
			rows: []ArchiveTrade{
				{Book: "btc_mxn", TID: 1, Price: 100, Amount: 1, ExchangeTS: base.Add(time.Minute)},
				{Book: "btc_mxn", TID: 1, Price: 100, Amount: 1, ExchangeTS: base.Add(time.Minute)},
				{Book: "btc_mxn", TID: 2, Price: 101, Amount: 1, ExchangeTS: base.Add(2 * time.Minute)},
			},
			wantTIDs: []int64{1, 2},
			wantDup:  1,
		},
		{
			name: "drops rows outside the window (day-prefix padding overshoot)",
			rows: []ArchiveTrade{
				{Book: "btc_mxn", TID: 1, Price: 100, Amount: 1, ExchangeTS: base.Add(-time.Minute)},
				{Book: "btc_mxn", TID: 2, Price: 100, Amount: 1, ExchangeTS: base.Add(time.Minute)},
				{Book: "btc_mxn", TID: 3, Price: 100, Amount: 1, ExchangeTS: to.Add(time.Minute)},
			},
			wantTIDs: []int64{2},
			wantOOR:  2,
		},
		{
			name: "drops non-trades rather than letting them bias indicators",
			rows: []ArchiveTrade{
				{Book: "btc_mxn", TID: 1, Price: 0, Amount: 1, ExchangeTS: base.Add(time.Minute)},
				{Book: "btc_mxn", TID: 2, Price: 100, Amount: 0, ExchangeTS: base.Add(time.Minute)},
				{Book: "btc_mxn", TID: 3, Price: -5, Amount: 1, ExchangeTS: base.Add(time.Minute)},
				{Book: "btc_mxn", TID: 4, Price: 100, Amount: 1, ExchangeTS: time.Time{}},
				{Book: "btc_mxn", TID: 5, Price: 100, Amount: 1, ExchangeTS: base.Add(time.Minute)},
			},
			wantTIDs: []int64{5},
			wantBad:  4,
		},
		{
			name: "drops rows for another book",
			rows: []ArchiveTrade{
				{Book: "eth_mxn", TID: 1, Price: 100, Amount: 1, ExchangeTS: base.Add(time.Minute)},
				{Book: "btc_mxn", TID: 2, Price: 100, Amount: 1, ExchangeTS: base.Add(time.Minute)},
			},
			wantTIDs: []int64{2},
			wantBad:  1,
		},
		{
			name: "sorts by exchange_ts then tid",
			rows: []ArchiveTrade{
				{Book: "btc_mxn", TID: 9, Price: 100, Amount: 1, ExchangeTS: base.Add(3 * time.Minute)},
				{Book: "btc_mxn", TID: 5, Price: 100, Amount: 1, ExchangeTS: base.Add(time.Minute)},
				{Book: "btc_mxn", TID: 4, Price: 100, Amount: 1, ExchangeTS: base.Add(time.Minute)},
				{Book: "btc_mxn", TID: 7, Price: 100, Amount: 1, ExchangeTS: base.Add(2 * time.Minute)},
			},
			wantTIDs: []int64{4, 5, 7, 9},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var st Stats
			got := Normalize(tc.rows, "btc_mxn", from, to, &st)

			var gotTIDs []int64
			for _, r := range got {
				gotTIDs = append(gotTIDs, r.TID)
			}
			if fmt.Sprint(gotTIDs) != fmt.Sprint(tc.wantTIDs) {
				t.Errorf("tids = %v, want %v", gotTIDs, tc.wantTIDs)
			}
			if st.DuplicatesResult != tc.wantDup {
				t.Errorf("duplicates = %d, want %d", st.DuplicatesResult, tc.wantDup)
			}
			if st.OutOfRange != tc.wantOOR {
				t.Errorf("out of range = %d, want %d", st.OutOfRange, tc.wantOOR)
			}
			if st.Invalid != tc.wantBad {
				t.Errorf("invalid = %d, want %d", st.Invalid, tc.wantBad)
			}
			if st.TradesReturned != len(tc.wantTIDs) {
				t.Errorf("returned = %d, want %d", st.TradesReturned, len(tc.wantTIDs))
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Day-prefix enumeration
// ---------------------------------------------------------------------------

func TestDayPrefixes(t *testing.T) {
	from := time.Date(2026, 8, 30, 23, 0, 0, 0, time.UTC)
	to := time.Date(2026, 9, 2, 1, 0, 0, 0, time.UTC)

	got := DayPrefixes("trades", "btc_mxn", from, to)
	want := []string{
		"trades/book=btc_mxn/year=2026/month=08/day=30/",
		"trades/book=btc_mxn/year=2026/month=08/day=31/",
		"trades/book=btc_mxn/year=2026/month=09/day=01/",
		"trades/book=btc_mxn/year=2026/month=09/day=02/",
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("prefixes:\n got %v\nwant %v", got, want)
	}
}

func TestDayPrefixes_InvertedWindowYieldsNothing(t *testing.T) {
	from := time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	if got := DayPrefixes("trades", "btc_mxn", from, to); len(got) != 0 {
		t.Fatalf("got %v, want none", got)
	}
}

// ---------------------------------------------------------------------------
// S3Archive end-to-end over fixtures
// ---------------------------------------------------------------------------

func TestS3Archive_LoadTrades_ManySmallFilesPerDay(t *testing.T) {
	// Mirrors the archive's real, uncompacted shape: many tiny objects inside
	// a single day partition. The loader must not assume one file per day.
	day := time.Date(2026, 8, 21, 0, 0, 0, 0, time.UTC)
	store := newMemStore()

	tid := int64(1)
	for i := 0; i < 5; i++ {
		ts := day.Add(time.Duration(i) * time.Hour)
		key := fmt.Sprintf("trades/book=btc_mxn/year=2026/month=08/day=21/trades-2026082%dT%02d0000.000.parquet", 1, i)
		store.objects[key] = writeFixtureParquet(t, []parquetTrade{
			{Book: "btc_mxn", TID: tid, Price: 1_900_000 + float64(i*100), Amount: 0.01, MakerSide: "buy", ExchangeTS: ms(ts), ReceivedAt: ms(ts)},
			{Book: "btc_mxn", TID: tid + 1, Price: 1_900_050 + float64(i*100), Amount: 0.02, MakerSide: "sell", ExchangeTS: ms(ts.Add(time.Minute)), ReceivedAt: ms(ts.Add(time.Minute))},
		})
		tid += 2
	}

	a := NewS3Archive(store, ArchiveConfig{Bucket: "test-bucket", Prefix: "trades", Concurrency: 4})
	rows, st, err := a.LoadTrades(context.Background(), "btc_mxn", day, day.Add(24*time.Hour))
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if len(rows) != 10 {
		t.Fatalf("got %d trades, want 10", len(rows))
	}
	if st.ObjectsListed != 5 || st.ObjectsRead != 5 || st.ObjectsFailed != 0 {
		t.Errorf("object stats = listed:%d read:%d failed:%d, want 5/5/0", st.ObjectsListed, st.ObjectsRead, st.ObjectsFailed)
	}
	// Concurrency must not reorder the replay.
	for i := 1; i < len(rows); i++ {
		if rows[i].ExchangeTS.Before(rows[i-1].ExchangeTS) {
			t.Fatalf("trades out of order at %d: %s before %s", i, rows[i].ExchangeTS, rows[i-1].ExchangeTS)
		}
	}
}

func TestS3Archive_LoadTrades_CompactedSingleFilePerDay(t *testing.T) {
	// The same loader must work after the in-flight compaction lands, where a
	// day is one large object instead of thousands of small ones.
	day := time.Date(2026, 8, 21, 0, 0, 0, 0, time.UTC)
	store := newMemStore()

	var rowsIn []parquetTrade
	for i := 0; i < 500; i++ {
		ts := day.Add(time.Duration(i) * time.Minute)
		rowsIn = append(rowsIn, parquetTrade{
			Book: "btc_mxn", TID: int64(i + 1), Price: 1_900_000, Amount: 0.01,
			MakerSide: "buy", ExchangeTS: ms(ts), ReceivedAt: ms(ts),
		})
	}
	store.objects["trades/book=btc_mxn/year=2026/month=08/day=21/compacted-000.parquet"] = writeFixtureParquet(t, rowsIn)

	a := NewS3Archive(store, ArchiveConfig{Bucket: "test-bucket", Prefix: "trades"})
	rows, st, err := a.LoadTrades(context.Background(), "btc_mxn", day, day.Add(24*time.Hour))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(rows) != 500 {
		t.Fatalf("got %d trades, want 500", len(rows))
	}
	if st.ObjectsListed != 1 {
		t.Errorf("objects listed = %d, want 1", st.ObjectsListed)
	}
}

func TestS3Archive_LoadTrades_RecoversTradesFromABatchStraddlingMidnight(t *testing.T) {
	// The collector partitions on the FIRST trade of a flush batch, so a batch
	// starting at 23:59:59 on Aug 20 lands entirely under day=20 even though
	// most of its rows belong to Aug 21. Requesting only Aug 21 must still find
	// them, which is what the ±1 day listing pad is for.
	store := newMemStore()
	aug20 := time.Date(2026, 8, 20, 23, 59, 59, 0, time.UTC)
	aug21 := time.Date(2026, 8, 21, 0, 0, 0, 0, time.UTC)

	store.objects["trades/book=btc_mxn/year=2026/month=08/day=20/trades-20260821T000001.000.parquet"] = writeFixtureParquet(t, []parquetTrade{
		{Book: "btc_mxn", TID: 1, Price: 1_900_000, Amount: 0.01, MakerSide: "buy", ExchangeTS: ms(aug20), ReceivedAt: ms(aug20)},
		{Book: "btc_mxn", TID: 2, Price: 1_900_100, Amount: 0.01, MakerSide: "buy", ExchangeTS: ms(aug21.Add(time.Second)), ReceivedAt: ms(aug21.Add(time.Second))},
	})

	a := NewS3Archive(store, ArchiveConfig{Bucket: "test-bucket", Prefix: "trades"})
	rows, _, err := a.LoadTrades(context.Background(), "btc_mxn", aug21, aug21.Add(24*time.Hour))
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if len(rows) != 1 || rows[0].TID != 2 {
		t.Fatalf("got %+v, want exactly the Aug-21 row (tid 2) recovered from the day=20 prefix", rows)
	}
}

func TestS3Archive_LoadTrades_SkipsCorruptObjectButCountsIt(t *testing.T) {
	day := time.Date(2026, 8, 21, 0, 0, 0, 0, time.UTC)
	store := newMemStore()

	good := "trades/book=btc_mxn/year=2026/month=08/day=21/trades-a.parquet"
	bad := "trades/book=btc_mxn/year=2026/month=08/day=21/trades-b.parquet"
	store.objects[good] = writeFixtureParquet(t, []parquetTrade{
		{Book: "btc_mxn", TID: 1, Price: 1_900_000, Amount: 0.01, MakerSide: "buy", ExchangeTS: ms(day), ReceivedAt: ms(day)},
	})
	store.objects[bad] = []byte("truncated garbage")

	a := NewS3Archive(store, ArchiveConfig{Bucket: "test-bucket", Prefix: "trades"})
	rows, st, err := a.LoadTrades(context.Background(), "btc_mxn", day, day.Add(24*time.Hour))
	if err != nil {
		t.Fatalf("load should tolerate one corrupt object: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d trades, want 1", len(rows))
	}
	if st.ObjectsFailed != 1 {
		t.Errorf("ObjectsFailed = %d, want 1 — a skipped object must stay visible in the report", st.ObjectsFailed)
	}
}

func TestS3Archive_LoadTrades_StrictModeFailsOnCorruptObject(t *testing.T) {
	day := time.Date(2026, 8, 21, 0, 0, 0, 0, time.UTC)
	store := newMemStore()
	store.objects["trades/book=btc_mxn/year=2026/month=08/day=21/trades-b.parquet"] = []byte("garbage")

	strict := false
	a := NewS3Archive(store, ArchiveConfig{Bucket: "b", Prefix: "trades", SkipFailedObjects: &strict})
	if _, _, err := a.LoadTrades(context.Background(), "btc_mxn", day, day.Add(24*time.Hour)); err == nil {
		t.Fatal("expected strict mode to surface the decode error")
	}
}

func TestS3Archive_LoadTrades_EmptyArchiveIsNotAnError(t *testing.T) {
	day := time.Date(2026, 8, 21, 0, 0, 0, 0, time.UTC)
	a := NewS3Archive(newMemStore(), ArchiveConfig{Bucket: "b", Prefix: "trades"})
	rows, st, err := a.LoadTrades(context.Background(), "btc_mxn", day, day.Add(24*time.Hour))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 0 || st.TradesReturned != 0 {
		t.Fatalf("got %d rows, want 0", len(rows))
	}
}

func TestS3Archive_LoadTrades_ListErrorPropagates(t *testing.T) {
	day := time.Date(2026, 8, 21, 0, 0, 0, 0, time.UTC)
	store := newMemStore()
	store.listErr = errors.New("access denied")

	a := NewS3Archive(store, ArchiveConfig{Bucket: "b", Prefix: "trades"})
	if _, _, err := a.LoadTrades(context.Background(), "btc_mxn", day, day.Add(24*time.Hour)); err == nil {
		t.Fatal("expected list error to propagate — a silent empty result would look like an empty archive")
	}
}

// ---------------------------------------------------------------------------
// Provider seam
// ---------------------------------------------------------------------------

func TestLoadProvider_FeedsTheBacktestEngine(t *testing.T) {
	day := time.Date(2026, 8, 21, 0, 0, 0, 0, time.UTC)
	store := newMemStore()
	store.objects["trades/book=btc_mxn/year=2026/month=08/day=21/trades-a.parquet"] = writeFixtureParquet(t, []parquetTrade{
		{Book: "btc_mxn", TID: 2, Price: 1_900_200, Amount: 0.02, MakerSide: "sell", ExchangeTS: ms(day.Add(time.Minute)), ReceivedAt: ms(day.Add(time.Minute))},
		{Book: "btc_mxn", TID: 1, Price: 1_900_000, Amount: 0.01, MakerSide: "buy", ExchangeTS: ms(day), ReceivedAt: ms(day)},
	})

	a := NewS3Archive(store, ArchiveConfig{Bucket: "b", Prefix: "trades"})
	p, st, err := LoadProvider(context.Background(), a, "btc_mxn", day, day.Add(24*time.Hour))
	if err != nil {
		t.Fatalf("load provider: %v", err)
	}
	if p.TradeCount() != 2 {
		t.Fatalf("provider has %d trades, want 2", p.TradeCount())
	}
	if st.TradesReturned != 2 {
		t.Errorf("stats returned = %d, want 2", st.TradesReturned)
	}

	first := p.NextTrade()
	if first == nil || first.Price != 1_900_000 {
		t.Fatalf("first replayed trade = %+v, want the earliest (price 1_900_000)", first)
	}
	if first.Side != "sell" {
		t.Errorf("side = %q, want %q (taker is the opposite of maker_side=buy)", first.Side, "sell")
	}
}

// ---------------------------------------------------------------------------
// Postgres source: retention guard
// ---------------------------------------------------------------------------

type memScanner struct {
	rows []ArchiveTrade
	err  error
}

func (m *memScanner) QueryTrades(context.Context, string, time.Time, time.Time) ([]ArchiveTrade, error) {
	return m.rows, m.err
}
func (m *memScanner) Close() error { return nil }

func TestPostgresSource_RefusesWindowsOlderThanRetention(t *testing.T) {
	now := time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)
	src := NewPostgresSource(&memScanner{}, 7)
	src.now = func() time.Time { return now }

	// The full 34-day archive window is exactly the case that must fail loudly:
	// silently returning 7 days of data here would make a short backtest look
	// like a long one.
	from := now.AddDate(0, 0, -34)
	_, _, err := src.LoadTrades(context.Background(), "btc_mxn", from, now)

	var outside *ErrOutsideRetention
	if !errors.As(err, &outside) {
		t.Fatalf("got err %v, want ErrOutsideRetention", err)
	}
	if !strings.Contains(err.Error(), "S3 archive") {
		t.Errorf("error should point the caller at the S3 archive, got: %v", err)
	}
}

func TestPostgresSource_ServesWindowsInsideRetention(t *testing.T) {
	now := time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)
	from := now.AddDate(0, 0, -2)

	src := NewPostgresSource(&memScanner{rows: []ArchiveTrade{
		{Book: "btc_mxn", TID: 1, Price: 1_900_000, Amount: 0.01, MakerSide: "buy", ExchangeTS: from.Add(time.Hour)},
	}}, 7)
	src.now = func() time.Time { return now }

	rows, st, err := src.LoadTrades(context.Background(), "btc_mxn", from, now)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(rows) != 1 || st.TradesReturned != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}
}

func TestPostgresSource_DescribeAdvertisesTheLimit(t *testing.T) {
	d := NewPostgresSource(&memScanner{}, 7).Describe()
	if !strings.Contains(d, "LIMITED") {
		t.Errorf("Describe() = %q, want it to advertise the limited range", d)
	}
}
