# Pith

## What is this?

Pith is a small, local-first CLI for ingesting text chunks into a SQLite database with [sqlite-vec](https://github.com/asg017/sqlite-vec) and querying them semantically. It is meant for pipelines (for example, AST chunkers that emit NDJSON) and for keeping a codebase or notes searchable on disk without a hosted vector SaaS.

Embeddings are **not** stored magically: you provide an embedding backend. The default integration is **Ollama** (`/api/embed`), for example `nomic-embed-text`.

## Requirements

- [Go](https://go.dev/) 1.26+ (see `go.mod`)
- [Ollama](https://ollama.com/) with an embedding model pulled (e.g. `ollama pull nomic-embed-text`)
- This repo pins `github.com/ncruces/go-sqlite3` to **v0.20.3** so the WASM build bundled by `sqlite-vec-go-bindings` works with the embedded runtime (newer ncruces releases can hit WASM atomic instruction issues until bindings catch up).

## Install

```bash
go install github.com/devaldrete/pith/cmd/pith@latest
```

Or from a clone:

```bash
go build -o pith ./cmd/pith
```

## NDJSON contract (`pith ingest`)

Ingest reads **newline-delimited JSON** from stdin. Each line must be one JSON object with at least:

| Field         | Required | Meaning                          |
|---------------|----------|----------------------------------|
| `text`        | yes      | Chunk body to embed and store    |
| `path`        | yes      | Source path or logical id        |
| `start_line`  | no       | Start line (default `0`)         |
| `end_line`    | no       | End line (default `0`)           |
| `language`    | no       | Hint for tooling                 |
| `id`          | no       | Optional external id (not used as PK) |

Empty lines are skipped. Upserts use the unique key `(path, start_line, end_line)`.

## Environment

| Variable       | Effect                                      |
|----------------|---------------------------------------------|
| `PITH_DB`      | Default path for `--db` if you set it globally |
| `OLLAMA_HOST`  | Ollama base URL (default `http://127.0.0.1:11434`) |
| `DEBUG`        | Same as global `--debug` when supported by Kong mapping |

## Usage

Initialize a database (optional; `ingest` creates tables as needed):

```bash
pith init --db ./my.db
```

Pipeline ingest (example):

```bash
your-chunker --to-json ./src/ | pith ingest --db ./my.db --embed-model nomic-embed-text
```

Smoke test without a chunker:

```bash
echo '{"text":"hello world","path":"demo.txt","start_line":1,"end_line":1}' | pith ingest --db ./demo.db
```

Query (natural language; must use the **same** `--embed-model` as ingest):

```bash
pith query --db ./my.db "where is auth handled?"
```

Machine-readable hits:

```bash
pith query --db ./my.db --json --k 5 "error handling"
```

Default text output columns: `path`, `start:end`, `distance`, and a short snippet (tabs separated).

## How it works

- **Storage**: `chunks` table for metadata and text; `vec_chunks` sqlite-vec `vec0` virtual table for float embeddings. Model name and vector dimension are stored in `meta` after the first successful ingest.
- **Embeddings**: Batch calls to Ollama `POST /api/embed` with an `input` array of strings.
- **Search**: KNN via sqlite-vec `MATCH` + `k`, joined back to `chunks` for snippets.

## Why use this tool?

Local `.db` file, no account, suitable for AI-assisted workflows or plain human search. Combine with any tool that can emit the NDJSON shape above.
