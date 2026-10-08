package live

import (
	"context"
	"log"
	"math"
	"sort"
	"sync"
	"time"
)

// ProvisionalLabel is shown with every value computed from the forming bar.
const ProvisionalLabel = "provisional: if today closed now"

// Mexico is the time zone Bitso buckets daily candles in (00:00 Mexico City).
var Mexico = mustLoc("America/Mexico_City")

func mustLoc(name string) *time.Location {
	l, err := time.LoadLocation(name)
	if err != nil {
		// Mexico City has had no DST since 2022: UTC-6 is exact today.
		return time.FixedZone("CST", -6*3600)
	}
	return l
}

// Day is the Mexico City calendar date of t, i.e. its daily bucket.
func Day(t time.Time) string { return t.In(Mexico).Format("2006-01-02") }

// Candle is today's forming daily bar.
type Candle struct {
	Date       string  `json:"date"` // Mexico City day of the bucket
	Open       float64 `json:"open"`
	High       float64 `json:"high"`
	Low        float64 `json:"low"`
	Close      float64 `json:"close"`
	Volume     float64 `json:"volume"`
	TradeCount int     `json:"trade_count"`
	// Seeded is true once the bar was loaded from Bitso's REST OHLC, so it
	// covers the whole day and not only the trades seen since connecting.
	Seeded bool `json:"seeded"`
}

func (c *Candle) apply(t Trade) {
	if c.TradeCount == 0 && c.Open == 0 {
		c.Open, c.High, c.Low = t.Price, t.Price, t.Price
	}
	c.High = math.Max(c.High, t.Price)
	if c.Low == 0 || t.Price < c.Low {
		c.Low = t.Price
	}
	c.Close = t.Price
	c.Volume += t.Amount
	c.TradeCount++
}

// Seeder loads the current day's bar for a book from REST. found=false means
// Bitso has no trades for that day yet.
type Seeder func(ctx context.Context, book string, day string) (c Candle, found bool, err error)

// Closes returns the closed daily closes for a book (oldest first) and the
// date of the last one, from the executor's candle file.
type Closes func(book string) (closes []float64, lastDate string, err error)

// Provisional is the SMA50 rule evaluated as if the forming bar closed now.
//
// With S49 the sum of the last 49 closed closes, the SMA50 including a
// provisional close c is (S49 + c)/50, and c > (S49 + c)/50 exactly when
// c > S49/49. So the flip level is the mean of the last 49 closed closes.
type Provisional struct {
	Label       string  `json:"label"`
	Price       float64 `json:"price"`      // the provisional close (last trade)
	FlipLevel   float64 `json:"flip_level"` // long above, flat at or below
	SMA50       float64 `json:"sma50"`      // including the provisional close
	Signal      string  `json:"signal"`     // "long" | "flat"
	DistancePct float64 `json:"distance_to_flip_pct"`
	BasedOn     string  `json:"based_on"` // last closed bar used
}

// ComputeProvisional applies the formula above. It returns nil, with a
// reason, when the closes do not end on the day before the forming bar
// (the executor has not fetched yesterday's bar yet) or are too few.
func ComputeProvisional(closes []float64, lastDate, formingDay string, price float64) (*Provisional, string) {
	if price <= 0 {
		return nil, "no trade yet"
	}
	if len(closes) < 49 {
		return nil, "fewer than 49 closed bars"
	}
	fd, err := time.Parse("2006-01-02", formingDay)
	if err != nil {
		return nil, "bad forming day"
	}
	if want := fd.AddDate(0, 0, -1).Format("2006-01-02"); lastDate != want {
		return nil, "candle file ends " + lastDate + ", not " + want + " (waiting for the executor's next run)"
	}
	s49 := 0.0
	for _, c := range closes[len(closes)-49:] {
		s49 += c
	}
	flip := s49 / 49
	p := &Provisional{Label: ProvisionalLabel, Price: price, FlipLevel: flip, SMA50: (s49 + price) / 50,
		Signal: "flat", DistancePct: (price - flip) / price * 100, BasedOn: lastDate}
	if price > flip {
		p.Signal = "long"
	}
	return p, ""
}

// BookSnapshot is everything the UI shows live for one book.
type BookSnapshot struct {
	Book            string       `json:"book"`
	Last            float64      `json:"last"`
	LastSide        string       `json:"last_side"`
	LastAt          string       `json:"last_at"`
	Bid             float64      `json:"bid"`
	Ask             float64      `json:"ask"`
	Candle          *Candle      `json:"candle"`
	Provisional     *Provisional `json:"provisional"`
	ProvisionalNote string       `json:"provisional_note,omitempty"`
	UpdatedAt       string       `json:"updated_at"`
}

// Status is the upstream connection state.
type Status struct {
	Source        string `json:"source"`
	Connected     bool   `json:"connected"`
	Since         string `json:"since"`
	LastMessageAt string `json:"last_message_at"`
	Reconnects    int    `json:"reconnects"`
	LastError     string `json:"last_error,omitempty"`
}

// Snapshot is the full live state for a set of books. Markets is filled
// only when asked for (the Market page: ?market=1).
type Snapshot struct {
	GeneratedAt string         `json:"generated_at"`
	Upstream    Status         `json:"upstream"`
	Books       []BookSnapshot `json:"books"`
	Markets     []Market       `json:"markets,omitempty"`
}

// TapeSize is how many recent trades the tape keeps per book.
const TapeSize = 50

// TapeTrade is one trade on the tape.
type TapeTrade struct {
	ID     int64   `json:"id"`
	Price  float64 `json:"price"`
	Amount float64 `json:"amount"`
	Side   string  `json:"side"` // taker side: "buy" lifted the ask, "sell" hit the bid
	At     string  `json:"at"`
}

// Market is the Market page's view of one book: spread, the top levels and
// the tape. Display only, like everything in this package.
type Market struct {
	Book      string  `json:"book"`
	Bid       float64 `json:"bid"`
	Ask       float64 `json:"ask"`
	Mid       float64 `json:"mid"`
	Spread    float64 `json:"spread"`
	SpreadBps float64 `json:"spread_bps"` // spread / mid × 10⁴
	Bids      []Level `json:"bids"`       // best first, at most MaxDepth
	Asks      []Level `json:"asks"`
	DepthAt   string  `json:"depth_at"` // time of the orders message
	// Trades is newest first, at most TapeSize; seeded from REST on connect.
	Trades     []TapeTrade `json:"trades"`
	TapeSeeded bool        `json:"tape_seeded"`
}

// Event is one server-sent event.
type Event struct {
	Name string // "snapshot" | "book" | "status" | "heartbeat"
	Data any
}

type bookState struct {
	last     float64
	lastSide string
	lastAt   time.Time
	bid, ask float64
	candle   *Candle
	updated  time.Time
	// While a REST seed is in flight, trades are also kept here so they can
	// be re-applied on top of the seeded bar.
	seeding  bool
	seedFrom time.Time
	pending  []Trade
	// Market page: the top levels and the tape (newest first).
	bids, asks []Level
	depthAt    time.Time
	tape       []Trade
	tapeSeeded bool
}

// addTape inserts t (newest first, by time then id), ignoring a trade
// already on the tape, and keeps the newest TapeSize.
func (st *bookState) addTape(t Trade) {
	i := 0
	for ; i < len(st.tape); i++ {
		x := st.tape[i]
		if x.ID == t.ID {
			return
		}
		if t.At.After(x.At) || (t.At.Equal(x.At) && t.ID > x.ID) {
			break
		}
	}
	if i >= TapeSize {
		return
	}
	for _, x := range st.tape[i:] { // a duplicate further down (older time)
		if x.ID == t.ID {
			return
		}
	}
	st.tape = append(st.tape, Trade{})
	copy(st.tape[i+1:], st.tape[i:])
	st.tape[i] = t
	if len(st.tape) > TapeSize {
		st.tape = st.tape[:TapeSize]
	}
}

// Subscription receives events for some books. C is closed when the hub
// drops a subscriber that cannot keep up, or on Close.
type Subscription struct {
	C      chan Event
	books  map[string]bool
	market bool // also "market" events
}

// Hub is the shared live state; it implements Handler and fans out
// throttled events to SSE subscribers.
type Hub struct {
	Books     []string
	Source    string
	Seeder    Seeder
	Tape      TapeSeeder // optional: seeds the Market page's tape on connect
	Closes    Closes
	Throttle  time.Duration // at most one "book" event per book per Throttle
	Heartbeat time.Duration
	Log       *log.Logger
	Now       func() time.Time

	mu     sync.Mutex
	books  map[string]*bookState
	status Status
	dirty  map[string]bool
	subs   map[*Subscription]bool
	closed bool
	ctx    context.Context
}

// NewHub returns a hub for books with the defaults (1 s throttle, 15 s heartbeat).
func NewHub(books []string, source string, seeder Seeder, closes Closes, logger *log.Logger) *Hub {
	h := &Hub{Books: append([]string(nil), books...), Source: source, Seeder: seeder, Closes: closes,
		Throttle: time.Second, Heartbeat: 15 * time.Second, Log: logger, Now: time.Now}
	sort.Strings(h.Books)
	h.init()
	return h
}

func (h *Hub) init() {
	h.books = map[string]*bookState{}
	for _, b := range h.Books {
		h.books[b] = &bookState{}
	}
	h.dirty = map[string]bool{}
	h.subs = map[*Subscription]bool{}
	h.status.Source = h.Source
	if h.Log == nil {
		h.Log = log.Default()
	}
	if h.Now == nil {
		h.Now = time.Now
	}
	h.ctx = context.Background()
}

// Has reports whether the hub tracks book.
func (h *Hub) Has(book string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	_, ok := h.books[book]
	return ok
}

// Run broadcasts throttled updates and heartbeats until ctx is done.
func (h *Hub) Run(ctx context.Context) {
	h.mu.Lock()
	h.ctx = ctx
	h.mu.Unlock()
	tick := time.NewTicker(h.Throttle)
	defer tick.Stop()
	hb := time.NewTicker(h.Heartbeat)
	defer hb.Stop()
	for {
		select {
		case <-ctx.Done():
			h.Close()
			return
		case <-tick.C:
			h.flush()
		case <-hb.C:
			h.mu.Lock()
			ev := Event{Name: "heartbeat", Data: map[string]any{"time": h.Now().UTC().Format(time.RFC3339), "upstream": h.status}}
			h.broadcastLocked(ev, nil, false)
			h.mu.Unlock()
		}
	}
}

// flush sends one "book" event per book that changed since the last flush,
// and a "market" event for it to the subscribers that asked for markets.
func (h *Hub) flush() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.dirty) == 0 {
		return
	}
	names := make([]string, 0, len(h.dirty))
	for b := range h.dirty {
		names = append(names, b)
	}
	sort.Strings(names)
	h.dirty = map[string]bool{}
	wantMarket := false
	for s := range h.subs {
		wantMarket = wantMarket || s.market
	}
	for _, b := range names {
		h.broadcastLocked(Event{Name: "book", Data: h.bookSnapshotLocked(b)}, &b, false)
		if wantMarket {
			h.broadcastLocked(Event{Name: "market", Data: h.marketLocked(b)}, &b, true)
		}
	}
}

// broadcastLocked sends ev to the subscribers of book (all when nil); with
// marketOnly, only to those that asked for "market" events.
func (h *Hub) broadcastLocked(ev Event, book *string, marketOnly bool) {
	for s := range h.subs {
		if book != nil && !s.books[*book] {
			continue
		}
		if marketOnly && !s.market {
			continue
		}
		select {
		case s.C <- ev:
		default:
			// A client that cannot keep up is dropped; its EventSource
			// reconnects and starts again from a snapshot.
			delete(h.subs, s)
			close(s.C)
		}
	}
}

// Subscribe registers a subscriber for books (all books when empty).
func (h *Hub) Subscribe(books []string) *Subscription { return h.SubscribeWith(books, false) }

// SubscribeWith is Subscribe; with market it also receives "market" events.
func (h *Hub) SubscribeWith(books []string, market bool) *Subscription {
	h.mu.Lock()
	defer h.mu.Unlock()
	s := &Subscription{C: make(chan Event, 64), books: map[string]bool{}, market: market}
	if len(books) == 0 {
		books = h.Books
	}
	for _, b := range books {
		s.books[b] = true
	}
	if h.closed {
		close(s.C)
		return s
	}
	h.subs[s] = true
	return s
}

// Unsubscribe removes s (safe to call twice).
func (h *Hub) Unsubscribe(s *Subscription) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.subs[s] {
		delete(h.subs, s)
		close(s.C)
	}
}

// Close ends every subscription (server shutdown).
func (h *Hub) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.closed = true
	for s := range h.subs {
		delete(h.subs, s)
		close(s.C)
	}
}

// Snapshot returns the current state for books (all when empty).
func (h *Hub) Snapshot(books []string) Snapshot { return h.SnapshotWith(books, false) }

// SnapshotWith is Snapshot; with market it also fills Markets.
func (h *Hub) SnapshotWith(books []string, market bool) Snapshot {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(books) == 0 {
		books = h.Books
	}
	out := Snapshot{GeneratedAt: h.Now().UTC().Format(time.RFC3339), Upstream: h.status, Books: []BookSnapshot{}}
	if market {
		out.Markets = []Market{}
	}
	for _, b := range books {
		if _, ok := h.books[b]; ok {
			out.Books = append(out.Books, h.bookSnapshotLocked(b))
			if market {
				out.Markets = append(out.Markets, h.marketLocked(b))
			}
		}
	}
	return out
}

// marketLocked is the Market page's view of book b (copies, safe to send).
func (h *Hub) marketLocked(b string) Market {
	st := h.books[b]
	m := Market{Book: b, Bid: st.bid, Ask: st.ask, DepthAt: rfc(st.depthAt), TapeSeeded: st.tapeSeeded,
		Bids: append([]Level{}, st.bids...), Asks: append([]Level{}, st.asks...), Trades: make([]TapeTrade, 0, len(st.tape))}
	if st.bid > 0 && st.ask > 0 {
		m.Mid = (st.bid + st.ask) / 2
		m.Spread = st.ask - st.bid
		m.SpreadBps = m.Spread / m.Mid * 1e4
	}
	for _, t := range st.tape {
		m.Trades = append(m.Trades, TapeTrade{ID: t.ID, Price: t.Price, Amount: t.Amount, Side: t.Side, At: rfc(t.At)})
	}
	return m
}

func rfc(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}

func (h *Hub) bookSnapshotLocked(b string) BookSnapshot {
	st := h.books[b]
	bs := BookSnapshot{Book: b, Last: st.last, LastSide: st.lastSide, LastAt: rfc(st.lastAt), Bid: st.bid, Ask: st.ask,
		UpdatedAt: rfc(st.updated)}
	if st.candle != nil {
		c := *st.candle
		bs.Candle = &c
	}
	day := Day(h.Now())
	if bs.Candle != nil {
		day = bs.Candle.Date
	}
	if h.Closes == nil {
		bs.ProvisionalNote = "no closed candles configured"
		return bs
	}
	closes, lastDate, err := h.Closes(b)
	if err != nil {
		bs.ProvisionalNote = "closed candles: " + err.Error()
		return bs
	}
	bs.Provisional, bs.ProvisionalNote = ComputeProvisional(closes, lastDate, day, st.last)
	return bs
}

// --- Handler ---

// OnConnect marks the upstream connected and re-seeds today's bars (and the
// tape) from REST, so trades missed while disconnected are not lost.
func (h *Hub) OnConnect() {
	h.mu.Lock()
	now := h.Now()
	if h.status.Since != "" {
		h.status.Reconnects++
	}
	h.status.Connected, h.status.Since, h.status.LastError = true, rfc(now), ""
	ctx := h.ctx
	h.broadcastLocked(Event{Name: "status", Data: h.status}, nil, false)
	h.mu.Unlock()
	for _, b := range h.Books {
		go h.seed(ctx, b)
		go h.seedTape(ctx, b)
	}
}

// seedTape merges the last trades from REST into the tape (by trade id, so
// trades already received live are not doubled).
func (h *Hub) seedTape(ctx context.Context, book string) {
	if h.Tape == nil {
		return
	}
	sctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	trades, err := h.Tape(sctx, book)
	h.mu.Lock()
	defer h.mu.Unlock()
	if err != nil {
		h.Log.Printf("live: tape %s: %v (tape built from live trades only)", book, err)
		return
	}
	st := h.books[book]
	for _, t := range trades {
		if t.Book == book {
			st.addTape(t)
		}
	}
	st.tapeSeeded = true
	h.dirty[book] = true
}

func (h *Hub) seed(ctx context.Context, book string) {
	if h.Seeder == nil {
		return
	}
	h.mu.Lock()
	st := h.books[book]
	start := h.Now()
	day := Day(start)
	st.seeding, st.seedFrom, st.pending = true, start, nil
	h.mu.Unlock()

	sctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	c, found, err := h.Seeder(sctx, book, day)

	h.mu.Lock()
	defer h.mu.Unlock()
	pending := st.pending
	st.seeding, st.pending = false, nil
	if err != nil {
		h.Log.Printf("live: seed %s %s: %v (bar built from live trades only)", book, day, err)
		return
	}
	if st.candle != nil && st.candle.Date > day {
		return // a new day started while seeding
	}
	if !found {
		c = Candle{Date: day}
	}
	c.Date, c.Seeded = day, true
	// Trades from before the request are in the REST bar already.
	for _, t := range pending {
		if !t.At.Before(start) && Day(t.At) == day {
			c.apply(t)
		}
	}
	st.candle = &c
	if st.last == 0 && c.Close > 0 {
		st.last = c.Close
	}
	st.updated = h.Now()
	h.dirty[book] = true
}

// OnMessage records upstream liveness.
func (h *Hub) OnMessage(at time.Time) {
	h.mu.Lock()
	h.status.LastMessageAt = rfc(at)
	h.mu.Unlock()
}

// OnTrade updates the last price and the forming bar.
func (h *Hub) OnTrade(t Trade) {
	h.mu.Lock()
	defer h.mu.Unlock()
	st, ok := h.books[t.Book]
	if !ok {
		return
	}
	if !t.At.Before(st.lastAt) {
		st.last, st.lastSide, st.lastAt = t.Price, t.Side, t.At
	}
	day := Day(t.At)
	switch {
	case st.candle == nil || day > st.candle.Date:
		st.candle = &Candle{Date: day}
		st.candle.apply(t)
	case day == st.candle.Date:
		st.candle.apply(t)
	default:
		// A late trade for a day already rolled over: ignore it.
	}
	if st.seeding {
		st.pending = append(st.pending, t)
	}
	st.addTape(t)
	st.updated = h.Now()
	h.dirty[t.Book] = true
}

// OnTop updates the best bid and ask and the top levels.
func (h *Hub) OnTop(t Top) {
	h.mu.Lock()
	defer h.mu.Unlock()
	st, ok := h.books[t.Book]
	if !ok {
		return
	}
	if t.Bid > 0 {
		st.bid = t.Bid
	}
	if t.Ask > 0 {
		st.ask = t.Ask
	}
	// Each orders message is a full top-of-book snapshot; an empty side
	// (never seen for BTC) keeps the previous levels rather than blanking.
	if len(t.Bids) > 0 {
		st.bids = append([]Level(nil), t.Bids...)
	}
	if len(t.Asks) > 0 {
		st.asks = append([]Level(nil), t.Asks...)
	}
	if len(t.Bids) > 0 || len(t.Asks) > 0 {
		st.depthAt = t.At
	}
	st.updated = h.Now()
	h.dirty[t.Book] = true
}

// OnDisconnect marks the upstream down.
func (h *Hub) OnDisconnect(err error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.status.Connected = false
	if err != nil {
		h.status.LastError = err.Error()
	}
	h.broadcastLocked(Event{Name: "status", Data: h.status}, nil, false)
}
