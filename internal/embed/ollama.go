package embed

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

const defaultOllamaHost = "http://127.0.0.1:11434"

// OllamaClient calls Ollama's /api/embed endpoint.
type OllamaClient struct {
	BaseURL    string
	ModelName  string
	HTTPClient *http.Client
}

// NewOllama returns a client with resolved base URL and model name.
func NewOllama(model string) *OllamaClient {
	base := strings.TrimSuffix(os.Getenv("OLLAMA_HOST"), "/")
	if base == "" {
		base = defaultOllamaHost
	}
	return &OllamaClient{
		BaseURL:   base,
		ModelName: model,
		HTTPClient: &http.Client{
			Timeout: 10 * time.Minute,
		},
	}
}

type embedRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type embedResponse struct {
	Model            string      `json:"model"`
	Embeddings       [][]float64 `json:"embeddings"`
	Embedding        []float64   `json:"embedding"` // single-input fallback
	Dim              int         `json:"dimension"`
	PromptEvalCount  int         `json:"prompt_eval_count"`
	TotalDuration    int64       `json:"total_duration"`
	LoadDuration     int64       `json:"load_duration"`
	PromptEvalDur    int64       `json:"prompt_eval_duration"`
}

func (c *OllamaClient) Model() string { return c.ModelName }

// Embed implements Embedder.
func (c *OllamaClient) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	body, err := json.Marshal(embedRequest{Model: c.ModelName, Input: texts})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/api/embed", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ollama /api/embed: %s: %s", resp.Status, bytes.TrimSpace(raw))
	}

	var out embedResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("ollama response json: %w", err)
	}

	var vecs [][]float64
	if len(out.Embeddings) > 0 {
		vecs = out.Embeddings
	} else if len(out.Embedding) > 0 {
		vecs = [][]float64{out.Embedding}
	}
	if len(vecs) != len(texts) {
		return nil, fmt.Errorf("ollama: expected %d embeddings, got %d", len(texts), len(vecs))
	}

	outF32 := make([][]float32, len(vecs))
	for i, v := range vecs {
		outF32[i] = make([]float32, len(v))
		for j, x := range v {
			outF32[i][j] = float32(x)
		}
	}
	return outF32, nil
}
