package loader

import (
	"bytes"
	"fmt"
	"io"
	"time"

	"github.com/xitongsys/parquet-go-source/buffer"
	"github.com/xitongsys/parquet-go/reader"
)

// parquetTrade mirrors, field for field and tag for tag, the writer schema in
// services/data-collector/internal/sink/batcher.go on the
// feat/intraday-data-collector branch. It must not drift from that definition.
//
// Timestamps are INT64 TIMESTAMP_MILLIS because parquet-go cannot encode
// time.Time directly; the writer stores UnixMilli and we decode it back here.
type parquetTrade struct {
	Book       string  `parquet:"name=book, type=BYTE_ARRAY, convertedtype=UTF8, encoding=PLAIN_DICTIONARY"`
	TID        int64   `parquet:"name=tid, type=INT64"`
	Price      float64 `parquet:"name=price, type=DOUBLE"`
	Amount     float64 `parquet:"name=amount, type=DOUBLE"`
	MakerSide  string  `parquet:"name=maker_side, type=BYTE_ARRAY, convertedtype=UTF8, encoding=PLAIN_DICTIONARY"`
	ExchangeTS int64   `parquet:"name=exchange_ts, type=INT64, convertedtype=TIMESTAMP_MILLIS"`
	ReceivedAt int64   `parquet:"name=received_at, type=INT64, convertedtype=TIMESTAMP_MILLIS"`
}

// decodeBatchSize caps how many rows are pulled from parquet-go per Read call.
// The archive's objects are tiny (a few KB each at time of writing) but a
// compacted object could hold a full day, so we stream instead of allocating
// GetNumRows() entries up front.
const decodeBatchSize = 50_000

// DecodeParquetTrades decodes one archived Parquet object into ArchiveTrades.
//
// It is deliberately a pure []byte -> []ArchiveTrade function with no S3
// coupling, so fixtures in the unit tests exercise exactly the same code path
// that production S3 objects take.
func DecodeParquetTrades(data []byte) ([]ArchiveTrade, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("empty parquet payload")
	}

	pf, err := buffer.NewBufferFile(data)
	if err != nil {
		return nil, fmt.Errorf("open parquet buffer: %w", err)
	}
	defer pf.Close()

	pr, err := reader.NewParquetReader(pf, new(parquetTrade), 4)
	if err != nil {
		return nil, fmt.Errorf("parquet reader: %w", err)
	}
	defer pr.ReadStop()

	total := int(pr.GetNumRows())
	if total <= 0 {
		return nil, nil
	}

	out := make([]ArchiveTrade, 0, total)
	for remaining := total; remaining > 0; {
		n := decodeBatchSize
		if remaining < n {
			n = remaining
		}
		rows := make([]parquetTrade, n)
		if err := pr.Read(&rows); err != nil {
			if err == io.EOF {
				break
			}
			return nil, fmt.Errorf("parquet read rows: %w", err)
		}
		for _, r := range rows {
			out = append(out, ArchiveTrade{
				Book:       r.Book,
				TID:        r.TID,
				Price:      r.Price,
				Amount:     r.Amount,
				MakerSide:  r.MakerSide,
				ExchangeTS: time.UnixMilli(r.ExchangeTS).UTC(),
				ReceivedAt: time.UnixMilli(r.ReceivedAt).UTC(),
			})
		}
		remaining -= n
	}

	return out, nil
}

// DecodeParquetTradesFromReader is a convenience wrapper for callers holding a
// stream (e.g. an S3 GetObject body) rather than a byte slice. Parquet requires
// random access for its footer, so the stream is fully buffered first.
func DecodeParquetTradesFromReader(r io.Reader) ([]ArchiveTrade, error) {
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		return nil, fmt.Errorf("buffer parquet stream: %w", err)
	}
	return DecodeParquetTrades(buf.Bytes())
}
