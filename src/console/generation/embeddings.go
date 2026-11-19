package generation

import (
	"bytes"
	"context"
	"cymonkey/src/console"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/webong/ext/res/int/inference"
)

// Embed supports explicit OpenAI-compatible, Cohere and Voyage API roots.
// Batch vectors are restored to input order and validated before returning.
func (c *Client) Embed(ctx context.Context, r inference.EmbeddingRequest) (inference.EmbeddingResponse, error) {
	if err := r.Validate(); err != nil {
		return inference.EmbeddingResponse{}, err
	}
	options, err := console.DecodeEmbeddingOptions(r.Options)
	if err != nil {
		return inference.EmbeddingResponse{}, err
	}
	payload := map[string]any{"model": r.Model}
	path := "/embeddings"
	switch c.config.Protocol {
	case OpenAI, Responses:
		payload["input"] = r.Inputs
		payload["encoding_format"] = "float"
		if options.Dimensions > 0 {
			payload["dimensions"] = options.Dimensions
		}
	case Cohere:
		if options.Dimensions > 0 || c.config.EmbeddingInputType == "" {
			return inference.EmbeddingResponse{}, unsupported("Cohere embeddings need an explicitly configured input type; dimensions are unsupported")
		}
		path = "/embed"
		payload["texts"] = r.Inputs
		payload["input_type"] = c.config.EmbeddingInputType
		payload["embedding_types"] = []string{"float"}
		payload["truncate"] = "NONE"
	case Voyage:
		payload["input"] = r.Inputs
		payload["truncation"] = false
		if options.Dimensions > 0 {
			payload["output_dimension"] = options.Dimensions
		}
		if c.config.EmbeddingInputType != "" {
			payload["input_type"] = c.config.EmbeddingInputType
		}
	default:
		return inference.EmbeddingResponse{}, unsupported("embeddings for this protocol")
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return inference.EmbeddingResponse{}, err
	}
	response, err := c.do(ctx, http.MethodPost, path, "application/json", bytes.NewReader(body), false)
	if err != nil {
		return inference.EmbeddingResponse{}, err
	}
	defer response.Body.Close()
	data, err := readBounded(response.Body, c.config.MaxResponseBytes)
	if err != nil {
		return inference.EmbeddingResponse{}, err
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
		return inference.EmbeddingResponse{}, err
	}
	result := inference.EmbeddingResponse{Model: r.Model, Usage: inference.TokenUsage{Input: wire.Usage.Input, Total: wire.Usage.Total}}
	if wire.Model != "" {
		result.Model = wire.Model
	}
	if c.config.Protocol == Voyage {
		result.Usage.Input = result.Usage.Total
	}
	if c.config.Protocol == Cohere {
		for index, vector := range wire.Embeddings.Float {
			result.Embeddings = append(result.Embeddings, inference.Embedding{Index: index, Vector: vector})
		}
		result.Usage.Input = wire.Meta.Billed.Input
		result.Usage.Total = result.Usage.Input
	} else {
		count := len(r.Inputs)
		if len(wire.Data) != count {
			return result, errors.New("embedding response count differs from input")
		}
		vectors := make([][]float64, count)
		for _, entry := range wire.Data {
			if entry.Index < 0 || entry.Index >= count || vectors[entry.Index] != nil {
				return result, errors.New("invalid or duplicate embedding index")
			}
			vectors[entry.Index] = entry.Embedding
		}
		for index, vector := range vectors {
			result.Embeddings = append(result.Embeddings, inference.Embedding{Index: index, Vector: vector})
		}
	}
	return result, console.ValidateEmbeddings(result, r)
}
