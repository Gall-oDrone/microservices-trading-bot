package sink

import (
	"bytes"
	"fmt"
	"time"

	"bitso-trading-platform/data-collector/internal/models"

	"github.com/xitongsys/parquet-go/parquet"
	"github.com/xitongsys/parquet-go/reader"
	"github.com/xitongsys/parquet-go/source"
	"github.com/xitongsys/parquet-go/writer"
)

// parquetTrade is the on-disk Parquet schema. parquet-go cannot encode Go
// time.Time directly, so timestamps are stored as epoch milliseconds (INT64
// TIMESTAMP_MILLIS).
type parquetTrade struct {
	Book       string  `parquet:"name=book, type=BYTE_ARRAY, convertedtype=UTF8, encoding=PLAIN_DICTIONARY"`
	TID        int64   `parquet:"name=tid, type=INT64"`
	Price      float64 `parquet:"name=price, type=DOUBLE"`
	Amount     float64 `parquet:"name=amount, type=DOUBLE"`
	MakerSide  string  `parquet:"name=maker_side, type=BYTE_ARRAY, convertedtype=UTF8, encoding=PLAIN_DICTIONARY"`
	ExchangeTS int64   `parquet:"name=exchange_ts, type=INT64, convertedtype=TIMESTAMP_MILLIS"`
	ReceivedAt int64   `parquet:"name=received_at, type=INT64, convertedtype=TIMESTAMP_MILLIS"`
}

func toParquetTrade(t models.Trade) parquetTrade {
	return parquetTrade{
		Book:       t.Book,
		TID:        t.TID,
		Price:      t.Price,
		Amount:     t.Amount,
		MakerSide:  t.MakerSide,
		ExchangeTS: t.ExchangeTS.UTC().UnixMilli(),
		ReceivedAt: t.ReceivedAt.UTC().UnixMilli(),
	}
}

func fromParquetTrade(p parquetTrade) models.Trade {
	return models.Trade{
		Book:       p.Book,
		TID:        p.TID,
		Price:      p.Price,
		Amount:     p.Amount,
		MakerSide:  p.MakerSide,
		ExchangeTS: time.UnixMilli(p.ExchangeTS).UTC(),
		ReceivedAt: time.UnixMilli(p.ReceivedAt).UTC(),
	}
}

// EncodeParquet serializes trades into a single Snappy-compressed Parquet file.
func EncodeParquet(trades []models.Trade) ([]byte, error) {
	if len(trades) == 0 {
		return nil, fmt.Errorf("empty batch")
	}
	buf := new(bytes.Buffer)
	pw, err := writer.NewParquetWriterFromWriter(buf, new(parquetTrade), 1)
	if err != nil {
		return nil, fmt.Errorf("parquet writer: %w", err)
	}
	pw.CompressionType = parquet.CompressionCodec_SNAPPY

	for i := range trades {
		if err := pw.Write(toParquetTrade(trades[i])); err != nil {
			_ = pw.WriteStop()
			return nil, fmt.Errorf("parquet write row: %w", err)
		}
	}
	if err := pw.WriteStop(); err != nil {
		return nil, fmt.Errorf("parquet write stop: %w", err)
	}
	return buf.Bytes(), nil
}

// DecodeParquet reads every row of a Parquet file written by EncodeParquet.
func DecodeParquet(data []byte) ([]models.Trade, error) {
	pr, err := reader.NewParquetReader(newBytesFile(data), new(parquetTrade), 1)
	if err != nil {
		return nil, fmt.Errorf("parquet reader: %w", err)
	}
	defer pr.ReadStop()

	n := int(pr.GetNumRows())
	rows := make([]parquetTrade, n)
	if n > 0 {
		if err := pr.Read(&rows); err != nil {
			return nil, fmt.Errorf("parquet read rows: %w", err)
		}
	}
	out := make([]models.Trade, len(rows))
	for i := range rows {
		out[i] = fromParquetTrade(rows[i])
	}
	return out, nil
}

// bytesFile is a read-only, in-memory source.ParquetFile.
type bytesFile struct {
	data []byte
	r    *bytes.Reader
}

var _ source.ParquetFile = (*bytesFile)(nil)

func newBytesFile(data []byte) *bytesFile {
	return &bytesFile{data: data, r: bytes.NewReader(data)}
}

func (f *bytesFile) Read(p []byte) (int, error) { return f.r.Read(p) }

func (f *bytesFile) Seek(offset int64, whence int) (int64, error) {
	return f.r.Seek(offset, whence)
}

func (f *bytesFile) Write(p []byte) (int, error) {
	return 0, fmt.Errorf("bytesFile is read-only")
}

func (f *bytesFile) Close() error { return nil }

// Open returns an independent reader over the same bytes; parquet-go opens
// one handle per column.
func (f *bytesFile) Open(string) (source.ParquetFile, error) {
	return newBytesFile(f.data), nil
}

func (f *bytesFile) Create(string) (source.ParquetFile, error) {
	return nil, fmt.Errorf("bytesFile is read-only")
}
