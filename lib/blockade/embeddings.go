package blockade

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
)

type EmbeddingRequest struct {
	Model      string   `json:"model"`
	Texts      []string `json:"texts,omitempty"`
	Tokens     [][]int  `json:"tokens,omitempty"`
	Dimensions int      `json:"dimensions,omitempty"`
}
type EmbeddingResponse struct {
	Model   string      `json:"model"`
	Vectors [][]float64 `json:"vectors"`
	Usage   TokenUsage  `json:"usage"`
}
type Embedder interface {
	Embed(context.Context, EmbeddingRequest) (EmbeddingResponse, error)
}

func ValidateEmbeddingRequest(r EmbeddingRequest) error {
	if strings.TrimSpace(r.Model) == "" || r.Dimensions < 0 || r.Dimensions > 65536 || (len(r.Texts) == 0) == (len(r.Tokens) == 0) || len(r.Texts)+len(r.Tokens) > 2048 {
		return fmt.Errorf("%w: embedding model, input or dimensions", ErrInvalidGeneration)
	}
	size := 0
	for _, text := range r.Texts {
		if text == "" {
			return fmt.Errorf("%w: empty embedding text", ErrInvalidGeneration)
		}
		size += len(text)
		if size > 8<<20 {
			return fmt.Errorf("%w: embedding input exceeds 8 MiB", ErrInvalidGeneration)
		}
	}
	for _, tokens := range r.Tokens {
		if len(tokens) == 0 {
			return fmt.Errorf("%w: empty embedding tokens", ErrInvalidGeneration)
		}
		if len(tokens) > ((8<<20)-size)/8 {
			return fmt.Errorf("%w: embedding input exceeds 8 MiB", ErrInvalidGeneration)
		}
		size += len(tokens) * 8
		for _, token := range tokens {
			if token < 0 {
				return fmt.Errorf("%w: negative token", ErrInvalidGeneration)
			}
		}
	}
	if size > 8<<20 {
		return fmt.Errorf("%w: embedding input exceeds 8 MiB", ErrInvalidGeneration)
	}
	return nil
}

func ValidateEmbeddingResponse(r EmbeddingResponse, request EmbeddingRequest) error {
	if err := ValidateEmbeddingRequest(request); err != nil {
		return err
	}
	if r.Model == "" || len(r.Vectors) != len(request.Texts)+len(request.Tokens) || r.Usage.Input < 0 || r.Usage.Output < 0 || r.Usage.Total < 0 {
		return fmt.Errorf("%w: embedding response metadata", ErrInvalidGeneration)
	}
	dim := request.Dimensions
	if dim == 0 && len(r.Vectors) > 0 {
		dim = len(r.Vectors[0])
	}
	if dim == 0 || dim > 65536 || dim*len(r.Vectors) > 4<<20 {
		return fmt.Errorf("%w: embedding dimensions exceed bounds", ErrInvalidGeneration)
	}
	for _, vector := range r.Vectors {
		if len(vector) != dim {
			return fmt.Errorf("%w: inconsistent embedding dimensions", ErrInvalidGeneration)
		}
		for _, v := range vector {
			if math.IsNaN(v) || math.IsInf(v, 0) {
				return fmt.Errorf("%w: non-finite embedding", ErrInvalidGeneration)
			}
		}
	}
	return nil
}

// EmbedBatch performs only the requests supplied by the caller, preserving
// order. Concurrency is explicit and bounded; failures cancel remaining work.
func EmbedBatch(ctx context.Context, provider Embedder, requests []EmbeddingRequest, concurrency int) ([]EmbeddingResponse, error) {
	if ctx == nil || provider == nil || concurrency < 1 || concurrency > 32 || len(requests) > 1024 {
		return nil, errors.New("embedding batch needs a provider, at most 1024 requests and concurrency 1-32")
	}
	for _, r := range requests {
		if err := ValidateEmbeddingRequest(r); err != nil {
			return nil, err
		}
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	results := make([]EmbeddingResponse, len(requests))
	var mu sync.Mutex
	var firstErr error
	next := 0
	var workers sync.WaitGroup
	for worker := 0; worker < min(concurrency, len(requests)); worker++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for {
				mu.Lock()
				index := next
				next++
				mu.Unlock()
				if index >= len(requests) || ctx.Err() != nil {
					return
				}
				result, err := provider.Embed(ctx, requests[index])
				if err == nil {
					err = ValidateEmbeddingResponse(result, requests[index])
				}
				if err != nil {
					mu.Lock()
					if firstErr == nil {
						firstErr = fmt.Errorf("embedding request %d: %w", index, err)
					}
					mu.Unlock()
					cancel()
					return
				}
				results[index] = result
			}
		}()
	}
	workers.Wait()
	if firstErr != nil {
		return nil, firstErr
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return results, nil
}

// NormalizeVector and CosineSimilarity are pure helpers. Adapters do not
// silently normalize, truncate or otherwise alter requested embeddings.
func NormalizeVector(vector []float64) ([]float64, error) {
	scale := 0.0
	for _, v := range vector {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return nil, errors.New("non-finite vector")
		}
		scale = math.Max(scale, math.Abs(v))
	}
	if scale == 0 || len(vector) > 65536 {
		return nil, errors.New("empty, zero or oversized vector")
	}
	sum := 0.0
	for _, v := range vector {
		n := v / scale
		sum += n * n
	}
	norm := math.Sqrt(sum)
	out := make([]float64, len(vector))
	for i, v := range vector {
		out[i] = (v / scale) / norm
	}
	return out, nil
}
func CosineSimilarity(a, b []float64) (float64, error) {
	if len(a) != len(b) {
		return 0, errors.New("vector dimensions differ")
	}
	x, err := NormalizeVector(a)
	if err != nil {
		return 0, err
	}
	y, err := NormalizeVector(b)
	if err != nil {
		return 0, err
	}
	dot := 0.0
	for i := range x {
		dot += x[i] * y[i]
	}
	return math.Max(-1, math.Min(1, dot)), nil
}
