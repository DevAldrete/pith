package store

import (
	"testing"

	"github.com/devaldrete/pith/internal/ingest"
)

func TestMigrateUpsertSearch(t *testing.T) {
	c, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := Migrate(c); err != nil {
		t.Fatal(err)
	}
	if err := EnsureVecTable(c, 4); err != nil {
		t.Fatal(err)
	}
	if err := WriteEmbeddingMeta(c, EmbeddingMeta{Model: "test", Dim: 4}); err != nil {
		t.Fatal(err)
	}

	ch := &ingest.Chunk{Text: "alpha beta", Path: "f.go", StartLine: 1, EndLine: 5, Language: "go"}
	vec := []float32{1, 0, 0, 0}
	if err := UpsertChunk(c, ch, vec); err != nil {
		t.Fatal(err)
	}

	ch2 := &ingest.Chunk{Text: "gamma delta", Path: "f.go", StartLine: 10, EndLine: 12}
	vec2 := []float32{0, 1, 0, 0}
	if err := UpsertChunk(c, ch2, vec2); err != nil {
		t.Fatal(err)
	}

	q := []float32{0.9, 0.1, 0, 0}
	hits, err := Search(c, q, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) < 1 || hits[0].Path != "f.go" {
		t.Fatalf("hits: %+v", hits)
	}
}
