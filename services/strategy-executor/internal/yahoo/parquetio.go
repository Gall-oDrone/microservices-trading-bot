package yahoo

import (
	"fmt"
	"io"
	"os"

	"github.com/xitongsys/parquet-go-source/buffer"
	"github.com/xitongsys/parquet-go/parquet"
	"github.com/xitongsys/parquet-go/reader"
	"github.com/xitongsys/parquet-go/source"
	"github.com/xitongsys/parquet-go/writer"
)

// pageSize is larger than parquet-go's 8 KiB default: ZSTD compresses each
// page independently, and article bodies compress far better in 1 MiB pages.
const pageSize = 1 << 20

// writeParquet encodes rows (a []T of a parquet-tagged struct) with ZSTD and
// stores meta as footer key/value pairs, so the file documents its own
// lineage and dedup stats without a sidecar manifest.
func writeParquet[T any](w io.Writer, rows []T, meta map[string]string) error {
	pw, err := writer.NewParquetWriterFromWriter(w, new(T), 4)
	if err != nil {
		return fmt.Errorf("parquet writer: %w", err)
	}
	pw.CompressionType = parquet.CompressionCodec_ZSTD
	pw.PageSize = pageSize
	for i := range rows {
		if err := pw.Write(rows[i]); err != nil {
			return fmt.Errorf("parquet write row %d: %w", i, err)
		}
	}
	for _, k := range sortedKeys(meta) {
		v := meta[k]
		pw.Footer.KeyValueMetadata = append(pw.Footer.KeyValueMetadata, &parquet.KeyValue{Key: k, Value: &v})
	}
	if err := pw.WriteStop(); err != nil {
		return fmt.Errorf("parquet finish: %w", err)
	}
	return nil
}

// WritePricesFile writes the compacted stocks file.
func WritePricesFile(path string, rows []PriceRow, meta map[string]string) error {
	return writeFile(path, func(w io.Writer) error { return writeParquet(w, rows, meta) })
}

// WriteNewsFile writes the compacted news file.
func WriteNewsFile(path string, rows []NewsRow, meta map[string]string) error {
	return writeFile(path, func(w io.Writer) error { return writeParquet(w, rows, meta) })
}

// writeFile writes to a temp file and renames, so a failed run never leaves a
// truncated file where a reader (or an upload step) expects a complete one.
func writeFile(path string, fn func(io.Writer) error) error {
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if err := fn(f); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}

func readParquet[T any](pf source.ParquetFile) ([]T, map[string]string, error) {
	pr, err := reader.NewParquetReader(pf, new(T), 4)
	if err != nil {
		return nil, nil, fmt.Errorf("parquet reader: %w", err)
	}
	defer pr.ReadStop()
	meta := map[string]string{}
	for _, kv := range pr.Footer.KeyValueMetadata {
		if kv != nil && kv.Value != nil {
			meta[kv.Key] = *kv.Value
		}
	}
	n := int(pr.GetNumRows())
	rows := make([]T, n)
	if n > 0 {
		if err := pr.Read(&rows); err != nil {
			return nil, nil, fmt.Errorf("parquet read: %w", err)
		}
	}
	return rows, meta, nil
}

func readParquetFile[T any](path string) ([]T, map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	pf, err := buffer.NewBufferFile(data)
	if err != nil {
		return nil, nil, err
	}
	defer pf.Close()
	return readParquet[T](pf)
}

// ReadPricesFile loads a compacted stocks file and its footer metadata.
func ReadPricesFile(path string) ([]PriceRow, map[string]string, error) {
	return readParquetFile[PriceRow](path)
}

// ReadNewsFile loads a compacted news file and its footer metadata.
func ReadNewsFile(path string) ([]NewsRow, map[string]string, error) {
	return readParquetFile[NewsRow](path)
}
