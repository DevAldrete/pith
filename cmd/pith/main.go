package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/alecthomas/kong"

	"github.com/devaldrete/pith/internal/app"
	"github.com/devaldrete/pith/internal/embed"
)

type CLI struct {
	Debug bool `help:"Enable debug logging to stderr." env:"DEBUG"`

	Init   InitCmd   `cmd:"" help:"Create the SQLite database and schema."`
	Ingest IngestCmd `cmd:"" help:"Ingest NDJSON chunk lines from stdin."`
	Query  QueryCmd  `cmd:"" help:"Semantic search over ingested chunks."`
}

type InitCmd struct {
	DB string `help:"SQLite database path." default:"pith.db" env:"PITH_DB" name:"db" type:"path"`
}

func (c *InitCmd) Run(cli *CLI) error {
	return app.Init(c.DB, cli.Debug)
}

type IngestCmd struct {
	DB         string `help:"SQLite database path." default:"pith.db" env:"PITH_DB" name:"db" type:"path"`
	EmbedModel string `help:"Ollama embedding model name." default:"nomic-embed-text" name:"embed-model"`
	BatchSize  int    `help:"Chunks per /api/embed request." default:"32" name:"batch-size"`
}

func (c *IngestCmd) Run(cli *CLI) error {
	emb := embed.NewOllama(c.EmbedModel)
	return app.Ingest(context.Background(), c.DB, os.Stdin, emb, c.BatchSize, cli.Debug)
}

type QueryCmd struct {
	DB         string   `help:"SQLite database path." default:"pith.db" env:"PITH_DB" name:"db" type:"path"`
	EmbedModel string   `help:"Ollama embedding model (must match ingest)." default:"nomic-embed-text" name:"embed-model"`
	K          int      `help:"Max results." default:"10" name:"k"`
	JSON       bool     `help:"Print hits as NDJSON." name:"json"`
	Text       []string `arg:"" optional:"" passthrough:"" name:"text" help:"Search query"`
}

func (c *QueryCmd) Run(cli *CLI) error {
	if len(c.Text) == 0 {
		return fmt.Errorf("query text is required")
	}
	q := strings.Join(c.Text, " ")
	emb := embed.NewOllama(c.EmbedModel)
	return app.Query(context.Background(), c.DB, q, c.K, c.JSON, os.Stdout, emb, cli.Debug)
}

func main() {
	signal.Ignore(syscall.SIGPIPE)

	cli := CLI{}
	ctx := kong.Parse(&cli,
		kong.Name("pith"),
		kong.Description("Local vector ingest and search using SQLite, sqlite-vec, and Ollama embeddings."),
		kong.UsageOnError(),
	)

	err := ctx.Run(&cli)
	ctx.FatalIfErrorf(err)
}
