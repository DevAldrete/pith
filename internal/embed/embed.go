package embed

import "context"

// Embedder turns text into float32 vectors.
type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
	Model() string
}
