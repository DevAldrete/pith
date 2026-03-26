package ingest

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

// Chunk is one line of NDJSON input for pith ingest.
type Chunk struct {
	Text      string `json:"text"`
	Path      string `json:"path"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
	Language  string `json:"language"`
	ID        string `json:"id"`
}

// Validate returns an error if required fields are missing.
func (c *Chunk) Validate() error {
	if c.Text == "" {
		return fmt.Errorf("missing required field \"text\"")
	}
	if c.Path == "" {
		return fmt.Errorf("missing required field \"path\"")
	}
	return nil
}

// Reader yields validated chunks from a newline-delimited JSON stream.
type Reader struct {
	sc *bufio.Scanner
}

// NewReader wraps r with a scanner suitable for large lines.
func NewReader(r io.Reader) *Reader {
	sc := bufio.NewScanner(r)
	buf := make([]byte, 0, 64*1024)
	sc.Buffer(buf, 1024*1024)
	return &Reader{sc: sc}
}

// Next decodes the next non-empty line into a Chunk.
// Returns io.EOF when the stream ends cleanly.
func (r *Reader) Next() (*Chunk, error) {
	for r.sc.Scan() {
		line := bytes.TrimSpace(r.sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var c Chunk
		if err := json.Unmarshal(line, &c); err != nil {
			return nil, fmt.Errorf("ndjson decode: %w", err)
		}
		if err := c.Validate(); err != nil {
			return nil, err
		}
		return &c, nil
	}
	if err := r.sc.Err(); err != nil {
		return nil, err
	}
	return nil, io.EOF
}
