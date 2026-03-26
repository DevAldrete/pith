package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/devaldrete/pith/internal/embed"
	"github.com/devaldrete/pith/internal/ingest"
	"github.com/devaldrete/pith/internal/store"
)

// Init creates the database file (parent dirs) and base schema.
func Init(dbPath string, debug bool) error {
	if err := ensureDBDir(dbPath); err != nil {
		return err
	}
	c, err := store.Open(dbPath)
	if err != nil {
		return err
	}
	defer c.Close()
	if err := store.Migrate(c); err != nil {
		return err
	}
	if debug {
		fmt.Fprintf(os.Stderr, "initialized %q\n", dbPath)
	}
	return nil
}

func ensureDBDir(dbPath string) error {
	if dbPath == "" || dbPath == ":memory:" {
		return nil
	}
	p := dbPath
	if strings.HasPrefix(p, "file:") {
		if i := strings.Index(p, "?"); i >= 0 {
			p = p[len("file:"):i]
		} else {
			p = strings.TrimPrefix(p, "file:")
		}
	}
	dir := filepath.Dir(p)
	if dir == "." || dir == "" {
		return nil
	}
	return os.MkdirAll(dir, 0o755)
}

// Ingest reads NDJSON from r, embeds with emb, and writes to dbPath.
func Ingest(ctx context.Context, dbPath string, r io.Reader, emb embed.Embedder, batchSize int, debug bool) error {
	if err := ensureDBDir(dbPath); err != nil {
		return err
	}
	c, err := store.Open(dbPath)
	if err != nil {
		return err
	}
	defer c.Close()
	if err := store.Migrate(c); err != nil {
		return err
	}

	meta, err := store.ReadEmbeddingMeta(c)
	if err != nil {
		return err
	}
	if meta.Model != "" && meta.Model != emb.Model() {
		return fmt.Errorf("database was embedded with model %q; refusing model %q", meta.Model, emb.Model())
	}

	nd := ingest.NewReader(r)
	var batch []*ingest.Chunk
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		texts := make([]string, len(batch))
		for i, ch := range batch {
			texts[i] = ch.Text
		}
		vecs, err := emb.Embed(ctx, texts)
		if err != nil {
			return err
		}
		if len(vecs) != len(batch) {
			return fmt.Errorf("embedder returned %d vectors for %d chunks", len(vecs), len(batch))
		}
		dim := len(vecs[0])
		for _, v := range vecs {
			if len(v) != dim {
				return fmt.Errorf("inconsistent embedding lengths: %d vs %d", len(v), dim)
			}
		}

		if meta.Dim > 0 && meta.Dim != dim {
			return fmt.Errorf("database dimension %d does not match embedding dimension %d", meta.Dim, dim)
		}

		exists, err := store.VecTableExists(c)
		if err != nil {
			return err
		}
		if !exists {
			if err := store.EnsureVecTable(c, dim); err != nil {
				return err
			}
			if err := store.WriteEmbeddingMeta(c, store.EmbeddingMeta{Model: emb.Model(), Dim: dim}); err != nil {
				return err
			}
			meta.Dim = dim
			meta.Model = emb.Model()
			if debug {
				fmt.Fprintf(os.Stderr, "created vec_chunks with dim=%d model=%q\n", dim, emb.Model())
			}
		}

		for i, ch := range batch {
			if err := store.UpsertChunk(c, ch, vecs[i]); err != nil {
				return fmt.Errorf("chunk %s:%d-%d: %w", ch.Path, ch.StartLine, ch.EndLine, err)
			}
		}
		if debug {
			fmt.Fprintf(os.Stderr, "ingested %d chunks\n", len(batch))
		}
		batch = batch[:0]
		return nil
	}

	for {
		ch, err := nd.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		batch = append(batch, ch)
		if len(batch) >= batchSize {
			if err := flush(); err != nil {
				return err
			}
		}
	}
	return flush()
}

// Query runs a semantic search and prints to w (or encodes JSON when jsonOut).
func Query(ctx context.Context, dbPath, q string, k int, jsonOut bool, w io.Writer, emb embed.Embedder, debug bool) error {
	c, err := store.Open(dbPath)
	if err != nil {
		return err
	}
	defer c.Close()
	if err := store.Migrate(c); err != nil {
		return err
	}

	meta, err := store.ReadEmbeddingMeta(c)
	if err != nil {
		return err
	}
	if meta.Model == "" {
		return fmt.Errorf("database has no embeddings yet; run pith ingest first")
	}
	if meta.Model != emb.Model() {
		return fmt.Errorf("database embedded with %q; use --embed-model %q for query", meta.Model, meta.Model)
	}

	exists, err := store.VecTableExists(c)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("vector table missing; run pith ingest first")
	}

	vecs, err := emb.Embed(ctx, []string{q})
	if err != nil {
		return err
	}
	if len(vecs) != 1 {
		return fmt.Errorf("expected one query embedding, got %d", len(vecs))
	}
	if len(vecs[0]) != meta.Dim {
		return fmt.Errorf("query embedding dim %d != database dim %d", len(vecs[0]), meta.Dim)
	}

	hits, err := store.Search(c, vecs[0], k)
	if err != nil {
		return err
	}
	if debug {
		fmt.Fprintf(os.Stderr, "%d hits\n", len(hits))
	}

	if jsonOut {
		enc := json.NewEncoder(w)
		enc.SetEscapeHTML(false)
		for _, h := range hits {
			rec := map[string]any{
				"path":       h.Path,
				"start_line": h.StartLine,
				"end_line":   h.EndLine,
				"distance":   h.Distance,
				"snippet":    snippet(h.Body, 200),
			}
			if err := enc.Encode(rec); err != nil {
				return err
			}
		}
		return nil
	}

	for _, h := range hits {
		fmt.Fprintf(w, "%s\t%d:%d\t%.6f\t%s\n", h.Path, h.StartLine, h.EndLine, h.Distance, snippet(h.Body, 120))
	}
	return nil
}

func snippet(s string, max int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.TrimSpace(s)
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}
