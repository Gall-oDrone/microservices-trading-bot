package live

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

// DefaultRESTURL is Bitso's production public REST API.
const DefaultRESTURL = "https://api.bitso.com"

type ohlcCandle struct {
	BucketStart int64  `json:"bucket_start_time"` // ms
	FirstRate   string `json:"first_rate"`
	LastRate    string `json:"last_rate"`
	MinRate     string `json:"min_rate"`
	MaxRate     string `json:"max_rate"`
	TradeCount  int    `json:"trade_count"`
	Volume      string `json:"volume"`
}

type ohlcResponse struct {
	Success bool         `json:"success"`
	Payload []ohlcCandle `json:"payload"`
	Error   *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// RESTSeeder loads the day's bar from GET /api/v3/ohlc (time_bucket=86400),
// the same endpoint the executor's candles come from. Bitso returns the
// in-progress bucket too; that is the one used here.
func RESTSeeder(baseURL string, client *http.Client) Seeder {
	if baseURL == "" {
		baseURL = DefaultRESTURL
	}
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	return func(ctx context.Context, book, day string) (Candle, bool, error) {
		d, err := time.ParseInLocation("2006-01-02", day, Mexico)
		if err != nil {
			return Candle{}, false, err
		}
		start := d.Add(-24 * time.Hour) // a little before, in case of bucket edge effects
		end := time.Now().Add(time.Minute)
		url := fmt.Sprintf("%s/api/v3/ohlc?book=%s&time_bucket=86400&start=%d&end=%d", baseURL, book, start.UnixMilli(), end.UnixMilli())
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return Candle{}, false, err
		}
		res, err := client.Do(req)
		if err != nil {
			return Candle{}, false, err
		}
		defer res.Body.Close()
		var r ohlcResponse
		if err := json.NewDecoder(res.Body).Decode(&r); err != nil {
			return Candle{}, false, fmt.Errorf("ohlc %s: HTTP %d: %w", book, res.StatusCode, err)
		}
		if !r.Success {
			msg := "unknown error"
			if r.Error != nil {
				msg = r.Error.Code + " " + r.Error.Message
			}
			return Candle{}, false, fmt.Errorf("ohlc %s: HTTP %d: %s", book, res.StatusCode, msg)
		}
		for _, c := range r.Payload {
			if Day(time.UnixMilli(c.BucketStart)) != day {
				continue
			}
			num := func(s string) float64 { f, _ := strconv.ParseFloat(s, 64); return f }
			out := Candle{Date: day, Open: num(c.FirstRate), High: num(c.MaxRate), Low: num(c.MinRate),
				Close: num(c.LastRate), Volume: num(c.Volume), TradeCount: c.TradeCount}
			if out.Close <= 0 {
				return Candle{}, false, fmt.Errorf("ohlc %s %s: bad bar %+v", book, day, c)
			}
			return out, true, nil
		}
		return Candle{}, false, nil
	}
}

// TapeSeeder loads the most recent trades for a book (any order).
type TapeSeeder func(ctx context.Context, book string) ([]Trade, error)

type restTrade struct {
	CreatedAt string `json:"created_at"`
	Amount    string `json:"amount"`
	MakerSide string `json:"maker_side"`
	Price     string `json:"price"`
	TID       int64  `json:"tid"`
}

// RESTTapeSeeder loads the last TapeSize trades from GET /api/v3/trades.
// The REST maker_side is the opposite of the taker side the tape shows.
func RESTTapeSeeder(baseURL string, client *http.Client) TapeSeeder {
	if baseURL == "" {
		baseURL = DefaultRESTURL
	}
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	return func(ctx context.Context, book string) ([]Trade, error) {
		url := fmt.Sprintf("%s/api/v3/trades?book=%s&limit=%d", baseURL, book, TapeSize)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		res, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		defer res.Body.Close()
		var r struct {
			Success bool        `json:"success"`
			Payload []restTrade `json:"payload"`
		}
		if err := json.NewDecoder(res.Body).Decode(&r); err != nil {
			return nil, fmt.Errorf("trades %s: HTTP %d: %w", book, res.StatusCode, err)
		}
		if !r.Success {
			return nil, fmt.Errorf("trades %s: HTTP %d: not successful", book, res.StatusCode)
		}
		out := make([]Trade, 0, len(r.Payload))
		for _, t := range r.Payload {
			p, err1 := strconv.ParseFloat(t.Price, 64)
			a, err2 := strconv.ParseFloat(t.Amount, 64)
			at, err3 := parseBitsoTime(t.CreatedAt)
			if err1 != nil || err2 != nil || err3 != nil || p <= 0 || t.TID <= 0 {
				continue
			}
			side := "buy"
			if t.MakerSide == "buy" {
				side = "sell"
			}
			out = append(out, Trade{Book: book, ID: t.TID, Price: p, Amount: a, Side: side, At: at})
		}
		return out, nil
	}
}

// parseBitsoTime reads Bitso's "2026-10-08T00:29:50+0000" (and RFC 3339).
func parseBitsoTime(s string) (time.Time, error) {
	for _, layout := range []string{"2006-01-02T15:04:05-0700", "2006-01-02T15:04:05.000-0700", time.RFC3339Nano} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("bad time %q", s)
}
