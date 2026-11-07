// Package wirecompression implements bounded, independent GZIP messages.
package wirecompression

import (
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"sync"
)

const Threshold = 256

var writers = sync.Pool{New: func() any { w, _ := gzip.NewWriterLevel(io.Discard, 6); return w }}

func Compress(raw []byte) ([]byte, error) {
	var out bytes.Buffer
	w := writers.Get().(*gzip.Writer)
	w.Reset(&out)
	_, err := w.Write(raw)
	closeErr := w.Close()
	w.Reset(io.Discard)
	writers.Put(w)
	if err != nil {
		return nil, err
	}
	if closeErr != nil {
		return nil, closeErr
	}
	return out.Bytes(), nil
}

// Decompress rejects truncated, corrupt, concatenated and oversized members
// before a caller can pass any bytes to a business parser.
func Decompress(packed []byte, limit int) ([]byte, error) {
	input := bytes.NewReader(packed)
	r, err := gzip.NewReader(input)
	if err != nil {
		return nil, err
	}
	r.Multistream(false)
	raw, err := io.ReadAll(io.LimitReader(r, int64(limit)+1))
	closeErr := r.Close()
	if err != nil {
		return nil, err
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if len(raw) > limit || input.Len() != 0 {
		return nil, errors.New("GZIP size limit or trailing member")
	}
	return raw, nil
}
