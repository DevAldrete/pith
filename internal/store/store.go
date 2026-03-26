// Package store opens SQLite with sqlite-vec (ncruces WASM) and manages chunks + vectors.
package store

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	_ "github.com/asg017/sqlite-vec-go-bindings/ncruces"
	"github.com/ncruces/go-sqlite3"

	"github.com/devaldrete/pith/internal/ingest"
)

const (
	metaEmbedModel = "embed_model"
	metaEmbedDim   = "embed_dim"
)

// Open opens or creates a database at path (URI form for ncruces).
func Open(path string) (*sqlite3.Conn, error) {
	if path == "" {
		path = "pith.db"
	}
	if !strings.HasPrefix(path, "file:") && !strings.HasPrefix(path, ":memory:") {
		path = "file:" + path + "?_pragma=busy_timeout(5000)"
	}
	return sqlite3.Open(path)
}

// Migrate creates application tables (not vec0; that is created once dim is known).
func Migrate(c *sqlite3.Conn) error {
	if err := c.Exec(`CREATE TABLE IF NOT EXISTS meta (
		key TEXT PRIMARY KEY NOT NULL,
		value TEXT NOT NULL
	);`); err != nil {
		return err
	}
	return c.Exec(`CREATE TABLE IF NOT EXISTS chunks (
		id INTEGER PRIMARY KEY,
		path TEXT NOT NULL,
		start_line INTEGER NOT NULL DEFAULT 0,
		end_line INTEGER NOT NULL DEFAULT 0,
		language TEXT,
		content_hash TEXT NOT NULL,
		body TEXT NOT NULL,
		UNIQUE(path, start_line, end_line)
	);`)
}

// MetaGet returns a meta value, or empty string if missing.
func MetaGet(c *sqlite3.Conn, key string) (string, error) {
	stmt, _, err := c.Prepare(`SELECT value FROM meta WHERE key = ?`)
	if err != nil {
		return "", err
	}
	defer stmt.Close()
	if err := stmt.BindText(1, key); err != nil {
		return "", err
	}
	if !stmt.Step() {
		if err := stmt.Err(); err != nil {
			return "", err
		}
		return "", nil
	}
	return stmt.ColumnText(0), stmt.Reset()
}

// MetaSet upserts a meta key.
func MetaSet(c *sqlite3.Conn, key, value string) error {
	stmt, _, err := c.Prepare(`INSERT INTO meta (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	if err := stmt.BindText(1, key); err != nil {
		return err
	}
	if err := stmt.BindText(2, value); err != nil {
		return err
	}
	return stmt.Exec()
}

// VecTableExists reports whether vec_chunks is present.
func VecTableExists(c *sqlite3.Conn) (bool, error) {
	stmt, _, err := c.Prepare(`SELECT 1 FROM sqlite_master WHERE type = 'table' AND name = 'vec_chunks'`)
	if err != nil {
		return false, err
	}
	defer stmt.Close()
	if !stmt.Step() {
		if err := stmt.Err(); err != nil {
			return false, err
		}
		return false, nil
	}
	return true, stmt.Reset()
}

// EnsureVecTable creates vec_chunks with the given float dimension if it does not exist.
func EnsureVecTable(c *sqlite3.Conn, dim int) error {
	if dim <= 0 {
		return fmt.Errorf("embedding dimension must be positive")
	}
	exists, err := VecTableExists(c)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	sql := fmt.Sprintf(`CREATE VIRTUAL TABLE vec_chunks USING vec0(
		chunk_id INTEGER PRIMARY KEY,
		embedding float[%d]
	);`, dim)
	return c.Exec(sql)
}

// EmbeddingMeta is stored after the first successful ingest.
type EmbeddingMeta struct {
	Model string
	Dim   int
}

// ReadEmbeddingMeta loads embed_model and embed_dim from meta.
func ReadEmbeddingMeta(c *sqlite3.Conn) (EmbeddingMeta, error) {
	var m EmbeddingMeta
	model, err := MetaGet(c, metaEmbedModel)
	if err != nil {
		return m, err
	}
	dimStr, err := MetaGet(c, metaEmbedDim)
	if err != nil {
		return m, err
	}
	if dimStr != "" {
		d, err := strconv.Atoi(dimStr)
		if err != nil {
			return m, fmt.Errorf("meta %s: %w", metaEmbedDim, err)
		}
		m.Dim = d
	}
	m.Model = model
	return m, nil
}

// WriteEmbeddingMeta records model and dimension (after vec table exists).
func WriteEmbeddingMeta(c *sqlite3.Conn, m EmbeddingMeta) error {
	if err := MetaSet(c, metaEmbedModel, m.Model); err != nil {
		return err
	}
	return MetaSet(c, metaEmbedDim, strconv.Itoa(m.Dim))
}

// UpsertChunk stores or updates a chunk and its vector (delete + insert into vec0).
func UpsertChunk(c *sqlite3.Conn, ch *ingest.Chunk, vec []float32) error {
	h := sha256.Sum256([]byte(ch.Text))
	hash := hex.EncodeToString(h[:])

	stmt, _, err := c.Prepare(`INSERT INTO chunks (path, start_line, end_line, language, content_hash, body)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(path, start_line, end_line) DO UPDATE SET
			language = excluded.language,
			content_hash = excluded.content_hash,
			body = excluded.body
		RETURNING id`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	if err := stmt.BindText(1, ch.Path); err != nil {
		return err
	}
	if err := stmt.BindInt(2, ch.StartLine); err != nil {
		return err
	}
	if err := stmt.BindInt(3, ch.EndLine); err != nil {
		return err
	}
	if err := stmt.BindText(4, ch.Language); err != nil {
		return err
	}
	if err := stmt.BindText(5, hash); err != nil {
		return err
	}
	if err := stmt.BindText(6, ch.Text); err != nil {
		return err
	}

	if !stmt.Step() {
		if err := stmt.Err(); err != nil {
			return err
		}
		return fmt.Errorf("upsert chunk: no row returned")
	}
	id := stmt.ColumnInt64(0)
	if err := stmt.Reset(); err != nil {
		return err
	}

	del, _, err := c.Prepare(`DELETE FROM vec_chunks WHERE chunk_id = ?`)
	if err != nil {
		return err
	}
	defer del.Close()
	if err := del.BindInt64(1, id); err != nil {
		return err
	}
	if err := del.Exec(); err != nil {
		return err
	}

	vecJSON, err := json.Marshal(vec)
	if err != nil {
		return err
	}

	ins, _, err := c.Prepare(`INSERT INTO vec_chunks (chunk_id, embedding) VALUES (?, vec_f32(?))`)
	if err != nil {
		return err
	}
	defer ins.Close()
	if err := ins.BindInt64(1, id); err != nil {
		return err
	}
	if err := ins.BindText(2, string(vecJSON)); err != nil {
		return err
	}
	return ins.Exec()
}

// SearchHit is one row from a vector search.
type SearchHit struct {
	Path      string
	StartLine int
	EndLine   int
	Body      string
	Distance  float64
}

// Search performs a KNN query and joins chunk text.
func Search(c *sqlite3.Conn, queryVec []float32, k int) ([]SearchHit, error) {
	if k <= 0 {
		k = 10
	}
	qJSON, err := json.Marshal(queryVec)
	if err != nil {
		return nil, err
	}

	sql := `SELECT c.path, c.start_line, c.end_line, c.body, v.distance
		FROM vec_chunks AS v
		INNER JOIN chunks AS c ON c.id = v.chunk_id
		WHERE v.embedding MATCH ? AND k = ?`

	stmt, _, err := c.Prepare(sql)
	if err != nil {
		return nil, err
	}
	defer stmt.Close()
	if err := stmt.BindText(1, string(qJSON)); err != nil {
		return nil, err
	}
	if err := stmt.BindInt(2, k); err != nil {
		return nil, err
	}

	var out []SearchHit
	for stmt.Step() {
		hit := SearchHit{
			Path:      stmt.ColumnText(0),
			StartLine: stmt.ColumnInt(1),
			EndLine:   stmt.ColumnInt(2),
			Body:      stmt.ColumnText(3),
			Distance:  stmt.ColumnFloat(4),
		}
		out = append(out, hit)
	}
	if err := stmt.Err(); err != nil {
		return nil, err
	}
	return out, stmt.Reset()
}
