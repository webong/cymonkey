package generation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"generationkit"
)

// Embed supports explicit OpenAI-compatible, Cohere and Voyage API roots.
// Batch vectors are restored to input order and validated before returning.
func (c *Client) Embed(ctx context.Context, r generationkit.EmbeddingRequest) (generationkit.EmbeddingResponse, error) {
	if err := generationkit.ValidateEmbeddingRequest(r); err != nil {
		return generationkit.EmbeddingResponse{}, err
	}
	payload := map[string]any{"model": r.Model}
	path := "/embeddings"
	switch c.config.Protocol {
	case OpenAI, Responses:
		if len(r.Tokens) > 0 {
			payload["input"] = r.Tokens
		} else {
			payload["input"] = r.Texts
		}
		payload["encoding_format"] = "float"
		if r.Dimensions > 0 {
			payload["dimensions"] = r.Dimensions
		}
	case Cohere:
		if len(r.Tokens) > 0 || r.Dimensions > 0 || c.config.EmbeddingInputType == "" {
			return generationkit.EmbeddingResponse{}, unsupported("Cohere embeddings need text and explicitly configured input type; dimensions are unsupported")
		}
		path = "/embed"
		payload["texts"] = r.Texts
		payload["input_type"] = c.config.EmbeddingInputType
		payload["embedding_types"] = []string{"float"}
		payload["truncate"] = "NONE"
	case Voyage:
		if len(r.Tokens) > 0 {
			return generationkit.EmbeddingResponse{}, unsupported("Voyage token input")
		}
		payload["input"] = r.Texts
		payload["truncation"] = false
		if r.Dimensions > 0 {
			payload["output_dimension"] = r.Dimensions
		}
		if c.config.EmbeddingInputType != "" {
			payload["input_type"] = c.config.EmbeddingInputType
		}
	default:
		return generationkit.EmbeddingResponse{}, unsupported("embeddings for this protocol")
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return generationkit.EmbeddingResponse{}, err
	}
	response, err := c.do(ctx, http.MethodPost, path, "application/json", bytes.NewReader(body), false)
	if err != nil {
		return generationkit.EmbeddingResponse{}, err
	}
	defer response.Body.Close()
	data, err := readBounded(response.Body, c.config.MaxResponseBytes)
	if err != nil {
		return generationkit.EmbeddingResponse{}, err
	}
	var wire struct {
		Model string `json:"model"`
		Data  []struct {
			Index     int       `json:"index"`
			Embedding []float64 `json:"embedding"`
		} `json:"data"`
		Embeddings struct {
			Float [][]float64 `json:"float"`
		} `json:"embeddings"`
		Usage struct {
			Input int64 `json:"prompt_tokens"`
			Total int64 `json:"total_tokens"`
		} `json:"usage"`
		Meta struct {
			Billed struct {
				Input int64 `json:"input_tokens"`
			} `json:"billed_units"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return generationkit.EmbeddingResponse{}, err
	}
	result := generationkit.EmbeddingResponse{Model: r.Model, Usage: generationkit.TokenUsage{Input: wire.Usage.Input, Total: wire.Usage.Total}}
	if wire.Model != "" {
		result.Model = wire.Model
	}
	if c.config.Protocol == Voyage {
		result.Usage.Input = result.Usage.Total
	}
	if c.config.Protocol == Cohere {
		result.Vectors = wire.Embeddings.Float
		result.Usage.Input = wire.Meta.Billed.Input
		result.Usage.Total = result.Usage.Input
	} else {
		count := len(r.Texts) + len(r.Tokens)
		if len(wire.Data) != count {
			return result, errors.New("embedding response count differs from input")
		}
		result.Vectors = make([][]float64, count)
		for _, entry := range wire.Data {
			if entry.Index < 0 || entry.Index >= count || result.Vectors[entry.Index] != nil {
				return result, errors.New("invalid or duplicate embedding index")
			}
			result.Vectors[entry.Index] = entry.Embedding
		}
	}
	return result, generationkit.ValidateEmbeddingResponse(result, r)
}
