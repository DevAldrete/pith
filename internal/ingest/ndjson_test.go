package ingest

import (
	"io"
	"strings"
	"testing"
)

func TestReaderNext(t *testing.T) {
	in := `{"text":"hello","path":"a.py","start_line":1,"end_line":2}
`
	r := NewReader(strings.NewReader(in))
	ch, err := r.Next()
	if err != nil {
		t.Fatal(err)
	}
	if ch.Text != "hello" || ch.Path != "a.py" || ch.StartLine != 1 || ch.EndLine != 2 {
		t.Fatalf("unexpected chunk: %+v", ch)
	}
	_, err = r.Next()
	if err != io.EOF {
		t.Fatalf("expected EOF, got %v", err)
	}
}

func TestReaderSkipsEmptyLines(t *testing.T) {
	in := "\n\n{\"text\":\"x\",\"path\":\"p\"}\n"
	r := NewReader(strings.NewReader(in))
	ch, err := r.Next()
	if err != nil {
		t.Fatal(err)
	}
	if ch.Text != "x" {
		t.Fatal(ch)
	}
}

func TestValidateMissingText(t *testing.T) {
	c := Chunk{Path: "p"}
	if err := c.Validate(); err == nil {
		t.Fatal("expected error")
	}
}
