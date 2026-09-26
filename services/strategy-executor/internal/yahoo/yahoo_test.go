package yahoo

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func day(s string) int32 {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return DaysFromDate(t)
}

func ptr[T any](v T) *T { return &v }

func writeTree(t *testing.T, files map[string]string, mod map[string]time.Time) string {
	t.Helper()
	dir := t.TempDir()
	for rel, body := range files {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		if m, ok := mod[rel]; ok {
			if err := os.Chtimes(p, m, m); err != nil {
				t.Fatal(err)
			}
		}
	}
	return dir
}

const priceHdr = "ref,book,date,open,high,low,close,adj_close,volume\n"

func TestPriceKindLayouts(t *testing.T) {
	cases := map[string]struct {
		kind SourceKind
		book string
	}{
		"btc-usd/2024/05/20/20240520-btc-usd.csv":                                {KindPriceLegacy, "btc-usd"},
		"book=btc-usd/year=2026/month=03/day=01/format=csv/20260301-btc-usd.csv": {KindPriceHive, "btc-usd"},
		"book=btc-usd/year=2024/month=09/day=01/20240901-btc-usd.csv":            {KindPriceHive, "btc-usd"},
		"2024-05-31_full_record.csv":                                             {KindPriceSnapshot, ""},
	}
	for rel, want := range cases {
		k, b := priceKind(rel)
		if k != want.kind || b != want.book {
			t.Errorf("%s: got (%s,%q) want (%s,%q)", rel, k, b, want.kind, want.book)
		}
	}
}

func TestPricesNewestObjectWinsAndIdenticalCounted(t *testing.T) {
	early := time.Date(2024, 5, 30, 12, 0, 0, 0, time.UTC)
	late := time.Date(2024, 6, 1, 2, 0, 0, 0, time.UTC)
	files := map[string]string{
		// Partial bar fetched intraday...
		"btc-usd/2024/05/30/20240530-btc-usd.csv": priceHdr + "https://finance.yahoo.com,btc-usd,2024-05-30,100,110,90,105,105,1000\n",
		// ...superseded by the snapshot written after the day closed.
		"2024-05-31_full_record.csv": priceHdr +
			"https://finance.yahoo.com,btc-usd,2024-05-30,100,120,90,115,115,2000\n" +
			"https://finance.yahoo.com,eth-usd,2024-05-30,10,11,9,10.5,10.5,50\n",
		// Exact copy of the eth bar in the hive layout.
		"book=eth-usd/year=2024/month=05/day=30/format=csv/20240530-eth-usd.csv": priceHdr + "https://finance.yahoo.com,eth-usd,2024-05-30,10,11,9,10.5,10.5,50\n",
		// Excluded and invalid rows.
		"book=test-book/year=2024/month=05/day=30/x.csv": priceHdr + "https://finance.yahoo.com,test-book,2024-05-30,1,1,1,1,1,1\n",
		"eth-usd/2024/05/31/20240531-eth-usd.csv":        priceHdr + "https://finance.yahoo.com,eth-usd,2024-05-31,nan,nan,nan,nan,nan,\n",
	}
	dir := writeTree(t, files, map[string]time.Time{
		"btc-usd/2024/05/30/20240530-btc-usd.csv": early,
		"2024-05-31_full_record.csv":              late,
	})
	cands, rs, err := ReadPrices(dir, "stocks/crypto", map[string]bool{"test-book": true})
	if err != nil {
		t.Fatal(err)
	}
	if rs.Excluded != 1 || rs.Invalid != 1 || rs.Files != 5 {
		t.Fatalf("read stats: %+v", rs)
	}
	rows, ds := DedupPrices(cands)
	if len(rows) != 2 || ds.Unique != 2 || ds.Dropped != 2 || ds.Identical != 1 || ds.Superseded != 1 || ds.ConflictKeys != 1 {
		t.Fatalf("dedup: %+v rows=%d", ds, len(rows))
	}
	if rows[0].Book != "btc-usd" || rows[0].Close != 115 || *rows[0].Volume != 2000 {
		t.Fatalf("newest version should win: %+v", rows[0])
	}
	if rows[0].SourceKey != "stocks/crypto/2024-05-31_full_record.csv" {
		t.Fatalf("lineage: %s", rows[0].SourceKey)
	}
	if rows[1].Book != "eth-usd" {
		t.Fatalf("sort order: %+v", rows)
	}
}

func TestPricesTimestampDateFormatAndCreatedAt(t *testing.T) {
	// Files written since ~2026-06 use a timestamp date and a created_at column.
	files := map[string]string{
		"book=btc-usd/year=2026/month=06/day=03/format=csv/20260603-btc-usd.csv": "ref,book,date,open,high,low,close,adj_close,volume,created_at\n" +
			"https://finance.yahoo.com,btc-usd,2026-06-03 00:00:00,66694.01,67402.93,64009.68,64014.37,64014.37,47411556213,2026-06-07 14:09:48.876089\n",
	}
	cands, rs, err := ReadPrices(writeTree(t, files, nil), "stocks/crypto", nil)
	if err != nil || rs.Invalid != 0 || len(cands) != 1 {
		t.Fatalf("err=%v stats=%+v n=%d", err, rs, len(cands))
	}
	p := cands[0].Row
	if p.Day().Format("2006-01-02") != "2026-06-03" || p.CreatedAt == nil ||
		time.UnixMilli(*p.CreatedAt).UTC().Format(time.RFC3339) != "2026-06-07T14:09:48Z" {
		t.Fatalf("parsed: %+v", p)
	}
}

func TestPricesLastRowWinsWithinOneObject(t *testing.T) {
	// The collector appends intraday snapshots of the same date; the last row
	// is the final bar (its close equals the next day's open).
	files := map[string]string{
		"book=aave-usd/year=2025/month=02/day=03/format=csv/20250203-aave-usd.csv": priceHdr +
			"https://finance.yahoo.com,aave-usd,2025-02-03,258.23,258.23,202.43,219.35,219.35,868705984\n" +
			"https://finance.yahoo.com,aave-usd,2025-02-03,258.23,258.23,202.43,216.58,216.58,873124736\n" +
			"https://finance.yahoo.com,aave-usd,2025-02-03,258.31,284.91,202.43,276.61,276.61,1419678376\n",
	}
	cands, _, err := ReadPrices(writeTree(t, files, nil), "stocks/crypto", nil)
	if err != nil {
		t.Fatal(err)
	}
	// Shuffle-independence: reverse the candidate order before deduplicating.
	for i, j := 0, len(cands)-1; i < j; i, j = i+1, j-1 {
		cands[i], cands[j] = cands[j], cands[i]
	}
	rows, ds := DedupPrices(cands)
	if len(rows) != 1 || rows[0].Close != 276.61 || ds.Superseded != 2 || ds.ConflictKeys != 1 {
		t.Fatalf("rows=%+v stats=%+v", rows, ds)
	}
}

func TestCoverageReportsGaps(t *testing.T) {
	rows := []PriceRow{
		{Book: "a", Date: day("2026-06-01")}, {Book: "a", Date: day("2026-06-02")},
		{Book: "a", Date: day("2026-06-06")}, {Book: "b", Date: day("2026-01-01")},
	}
	c := Coverage(rows)
	if len(c) != 2 || c[0].Missing != 3 || len(c[0].Gaps) != 1 || c[0].Gaps[0] != "2026-06-03..2026-06-05 (3d)" {
		t.Fatalf("coverage: %+v", c)
	}
	if c[1].Missing != 0 || c[1].Rows != 1 {
		t.Fatalf("coverage b: %+v", c[1])
	}
}

func TestCanonicalURL(t *testing.T) {
	cases := map[string]string{
		"https://finance.yahoo.com/news/x-175002669.html?pl2=topic-stream_fltrd-strs": "https://finance.yahoo.com/news/x-175002669.html",
		" HTTPS://Finance.Yahoo.com/news/x.html#top ":                                 "https://finance.yahoo.com/news/x.html",
		"https://finance.yahoo.com/news/x/":                                           "https://finance.yahoo.com/news/x",
	}
	for in, want := range cases {
		if got := CanonicalURL(in); got != want {
			t.Errorf("CanonicalURL(%q) = %q, want %q", in, got, want)
		}
	}
}

const newsHdr = "id,source,headline,href,summary,content,author,minsread,datetime,llm_ticker,llm_overall_sentiment,llm_confidence,llm_signal,llm_actionable,llm_entities,llm_error\n"

func TestNewsDedupByURLNotID(t *testing.T) {
	files := map[string]string{
		// Two DIFFERENT articles sharing one id: both must survive.
		// Article A also appears unscored in a roll-up and in the JSONL copy.
		"year=2026/month=03/day=16/format=csv/news_transformed_y2026_m03_d16.csv": newsHdr +
			`111,decrypt,Headline A,https://finance.yahoo.com/news/a-1.html?pl2=x,,"Body A` + "\n" + `line 2",Ann,2 min read,2026-03-16T12:00:00.000Z,BTC,0.5,0.8,bullish,True,"[""Bitcoin""]",nan` + "\n" +
			`111,decrypt,Headline B,https://finance.yahoo.com/news/b-2.html,,Body B,Bob,3 min read,2026-03-16T13:00:00.000Z,ETH,-0.4,0.7,bearish,False,[],None` + "\n",
		"year=2026/week=11/format=csv/news_transformed_y2026_w11.csv": newsHdr +
			`111,decrypt,Headline A,https://finance.yahoo.com/news/a-1.html,,Body A,Ann,2 min read,2026-03-16T12:00:00.000Z,BTC,None,None,,,[],` + "\n" +
			// Roll-up-only article: must be kept.
			`222,cnbc,Headline C,https://finance.yahoo.com/news/c-3.html,,Body C,Cat,1 min read,2026-03-15T08:00:00.000Z,BTC,0.1,0.9,neutral,True,[],` + "\n",
		"year=2026/month=03/day=16/format=jsonl/news_transformed_y2026_m03_d16.jsonl": `{"id": "999", "title": "Headline A", "summary": "", "body": "", "metadata": {"source": "decrypt", "datetime": "2026-03-16T12:00:00.000Z", "url": "https://finance.yahoo.com/news/a-1.html", "author": "Ann", "llm_ticker": "BTC", "llm_overall_sentiment": 0.9, "llm_confidence": 0.9, "llm_actionable": true, "llm_entities": ["Bitcoin"]}}` + "\n" +
			// JSONL-only article: must be kept.
			`{"id": "998", "title": "Headline D", "summary": "", "body": "", "metadata": {"source": "x", "datetime": "2026-03-16T14:00:00.000Z", "url": "https://finance.yahoo.com/news/d-4.html", "llm_overall_sentiment": 0.2, "llm_confidence": 0.5, "llm_sectors": ["crypto", "equities"]}}` + "\n",
	}
	dir := writeTree(t, files, nil)
	cands, rs, err := ReadNews(dir, "news/transformed/crypto/agentic=true")
	if err != nil {
		t.Fatal(err)
	}
	if rs.Files != 3 || rs.Rows != 6 || rs.FilesFailed != 0 {
		t.Fatalf("read stats: %+v", rs)
	}
	rows, ds := DedupNews(cands)
	if len(rows) != 4 {
		t.Fatalf("want 4 articles (A, B, C, D), got %d: %+v", len(rows), ds)
	}
	if ds.IDsSpanningURLs != 1 || ds.URLsWithManyIDs != 1 || ds.Dropped != 2 {
		t.Fatalf("dedup stats: %+v", ds)
	}
	byURL := map[string]NewsRow{}
	for _, r := range rows {
		byURL[r.Href] = r
	}
	a := byURL["https://finance.yahoo.com/news/a-1.html"]
	// Scored daily CSV beats the unscored roll-up and the (scored) JSONL copy.
	if a.SourceKey != "news/transformed/crypto/agentic=true/year=2026/month=03/day=16/format=csv/news_transformed_y2026_m03_d16.csv" {
		t.Fatalf("kept wrong version of A: %s", a.SourceKey)
	}
	if *a.Content != "Body A\nline 2" || *a.LLMOverallSentiment != 0.5 || !*a.LLMActionable || a.LLMError != nil || *a.LLMEntities != `["Bitcoin"]` {
		t.Fatalf("A parsed wrong: %+v", a)
	}
	b := byURL["https://finance.yahoo.com/news/b-2.html"]
	if b.ID != "111" || *b.LLMActionable || b.LLMError != nil {
		t.Fatalf("B parsed wrong: %+v", b)
	}
	d := byURL["https://finance.yahoo.com/news/d-4.html"]
	if d.Content != nil || *d.LLMSectors != `["crypto","equities"]` || d.Author != nil {
		t.Fatalf("D parsed wrong: %+v", d)
	}
	// Sorted by publication time: C (03-15) first.
	if rows[0].Href != "https://finance.yahoo.com/news/c-3.html" {
		t.Fatalf("sort order: first is %s", rows[0].Href)
	}
}

func TestBetterNewsPrefersScoredOverLayout(t *testing.T) {
	scoredJSONL := Candidate[NewsRow]{Row: NewsRow{LLMOverallSentiment: ptr(0.1)}, Kind: KindJSONL}
	unscoredDaily := Candidate[NewsRow]{Row: NewsRow{}, Kind: KindDailyCSV}
	if !betterNews(scoredJSONL, unscoredDaily) || betterNews(unscoredDaily, scoredJSONL) {
		t.Fatal("a scored row must beat an unscored one regardless of layout")
	}
}

func TestParquetRoundTrip(t *testing.T) {
	dir := t.TempDir()
	prices := []PriceRow{
		{Book: "btc-usd", Date: day("2026-03-01"), Open: 1, High: 2, Low: 0.5, Close: 1.5, AdjClose: 1.5, Volume: ptr(int64(40260968448)), Ref: "r", SourceKey: "k"},
		{Book: "btc-usd", Date: day("2026-03-02"), Open: 1, High: 2, Low: 0.5, Close: 1.6, AdjClose: 1.6, Ref: "r", SourceKey: "k"},
	}
	pp := filepath.Join(dir, "p.parquet")
	if err := WritePricesFile(pp, prices, map[string]string{"dataset": "p"}); err != nil {
		t.Fatal(err)
	}
	gotP, meta, err := ReadPricesFile(pp)
	if err != nil {
		t.Fatal(err)
	}
	if len(gotP) != 2 || meta["dataset"] != "p" || *gotP[0].Volume != 40260968448 || gotP[1].Volume != nil || gotP[1].Day().Format("2006-01-02") != "2026-03-02" {
		t.Fatalf("prices round trip: %+v %v", gotP, meta)
	}

	ts := time.Date(2026, 3, 16, 12, 0, 0, 0, time.UTC).UnixMilli()
	news := []NewsRow{
		{Href: "u1", ID: "1", Datetime: &ts, Content: ptr("héllo\nworld"), LLMOverallSentiment: ptr(-0.25), LLMActionable: ptr(false), SourceKey: "k"},
		{Href: "u2", ID: "2", SourceKey: "k"},
	}
	np := filepath.Join(dir, "n.parquet")
	if err := WriteNewsFile(np, news, nil); err != nil {
		t.Fatal(err)
	}
	gotN, _, err := ReadNewsFile(np)
	if err != nil {
		t.Fatal(err)
	}
	if len(gotN) != 2 || *gotN[0].Content != "héllo\nworld" || *gotN[0].LLMOverallSentiment != -0.25 || *gotN[0].LLMActionable ||
		!gotN[0].Time().Equal(time.UnixMilli(ts).UTC()) || gotN[1].Datetime != nil || gotN[1].Content != nil {
		t.Fatalf("news round trip: %+v", gotN)
	}
	if _, err := os.Stat(np + ".tmp"); !os.IsNotExist(err) {
		t.Fatal("temp file left behind")
	}
}
