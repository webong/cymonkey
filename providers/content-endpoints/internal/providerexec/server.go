package providerexec

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"blockade"
	"providerplugin"
	"providerplugin/wirebridge"
)

const maxFrame = 24 << 20

var contentWire = wirebridge.Mapping{
	PublicPrefix: "cymonkey.content.", InternalPrefix: "blockade.",
	ToInternal: map[string]string{
		"cymonkey.content-adapter/v1alpha1":    blockade.ProviderAdapterAPIVersion,
		"cymonkey.observation/v1alpha1":        blockade.APIVersion,
		"cymonkey.observation-result/v1alpha1": blockade.APIVersion,
		"cymonkey.content/v1alpha1":            blockade.ContentAPIVersion,
		"cymonkey.model/v1alpha1":              blockade.ModelManifestAPIVersion,
	},
	ToPublic: map[string]string{
		blockade.ProviderAdapterAPIVersion: "cymonkey.content-adapter/v1alpha1",
		blockade.APIVersion:                "cymonkey.observation-result/v1alpha1",
		blockade.ContentAPIVersion:         "cymonkey.content/v1alpha1",
		blockade.ModelManifestAPIVersion:   "cymonkey.model/v1alpha1",
	},
	KeysToInternal: map[string]string{"plugin": "provider"}, KeysToPublic: map[string]string{"provider": "plugin"},
}

type StartRequest struct {
	ID       string                   `json:"id"`
	Settings map[string]string        `json:"settings,omitempty"`
	Model    *blockade.InstalledModel `json:"model,omitempty"`
}

type Provider interface {
	Start(context.Context, StartRequest) error
	Interpret(context.Context, blockade.ProviderAdapterInterpretRequest, map[string]string) (blockade.ProviderAdapterInterpretResponse, error)
	Capabilities(context.Context) (blockade.ProviderAdapterCapabilities, error)
	Health(context.Context) (blockade.ProviderAdapterHealth, error)
	Close() error
}

type request struct {
	ID     uint64          `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

type failure struct {
	Code                   string `json:"code,omitempty"`
	Message                string `json:"message"`
	RetryAfterMilliseconds int64  `json:"retryAfterMilliseconds,omitempty"`
}

type response struct {
	ID     uint64   `json:"id"`
	Result any      `json:"result,omitempty"`
	Error  *failure `json:"error,omitempty"`
}

func Serve(ctx context.Context, manifest providerplugin.Manifest, provider Provider, input io.Reader, output io.Writer) error {
	if !manifest.Supports("blockade.provider", "v1alpha1") && !manifest.Supports("cymonkey.content", "v1alpha1") {
		return errors.New("plugin manifest does not declare a supported content contract")
	}
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 4096), maxFrame)
	writer := bufio.NewWriter(output)
	defer provider.Close()
	for scanner.Scan() {
		var request request
		if err := json.Unmarshal(scanner.Bytes(), &request); err != nil {
			return fmt.Errorf("decode plugin request: %w", err)
		}
		result, err := call(ctx, manifest, provider, request.Method, request.Params)
		response := response{ID: request.ID, Result: result}
		if err != nil {
			response.Result = nil
			response.Error = encodeFailure(err)
		}
		encoded, err := json.Marshal(response)
		if err != nil {
			return err
		}
		if _, err := writer.Write(append(encoded, '\n')); err != nil {
			return err
		}
		if err := writer.Flush(); err != nil {
			return err
		}
	}
	return scanner.Err()
}

func call(ctx context.Context, manifest providerplugin.Manifest, provider Provider, method string, raw json.RawMessage) (any, error) {
	return contentWire.WrapCall(method, raw, func(method string, raw json.RawMessage) (any, error) {
		return callLibrary(ctx, manifest, provider, method, raw)
	})
}

func callLibrary(ctx context.Context, manifest providerplugin.Manifest, provider Provider, method string, raw json.RawMessage) (any, error) {
	switch method {
	case "plugin.hello":
		return manifest, nil
	case "blockade.start":
		var params StartRequest
		if err := decodeParams(raw, &params); err != nil {
			return nil, invalidRequest(err)
		}
		if params.Model != nil {
			return nil, invalidRequest(errors.New("endpoint providers do not use Cymonkey-installed models"))
		}
		return map[string]any{}, provider.Start(ctx, params)
	case "blockade.capabilities":
		return provider.Capabilities(ctx)
	case "blockade.health":
		return provider.Health(ctx)
	case "blockade.interpret":
		var params struct {
			Request blockade.ProviderAdapterInterpretRequest `json:"request"`
			Secrets map[string]string                        `json:"secrets,omitempty"`
		}
		if err := decodeParams(raw, &params); err != nil {
			return nil, invalidRequest(err)
		}
		if params.Request.APIVersion != blockade.ProviderAdapterAPIVersion {
			return nil, invalidRequest(errors.New("unsupported provider adapter apiVersion"))
		}
		if err := blockade.ValidateInterpretRequest(params.Request.Request); err != nil {
			return nil, invalidRequest(err)
		}
		return provider.Interpret(ctx, params.Request, params.Secrets)
	case "blockade.close":
		return map[string]any{}, provider.Close()
	default:
		return nil, invalidRequest(fmt.Errorf("unsupported method %q", method))
	}
}

func decodeParams(raw json.RawMessage, destination any) error {
	if len(raw) == 0 || string(raw) == "null" {
		return errors.New("method parameters are required")
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return errors.New("method parameters must contain one JSON object")
	}
	return nil
}

func invalidRequest(err error) error {
	return blockade.NewProviderAdapterError(blockade.ProviderAdapterErrorInvalidRequest, err)
}

func encodeFailure(err error) *failure {
	result := &failure{Code: string(blockade.ProviderAdapterErrorUnavailable), Message: "provider failed"}
	var adapterError *blockade.ProviderAdapterError
	if !errors.As(err, &adapterError) {
		return result
	}
	result.Code = string(adapterError.Kind)
	result.Message = adapterError.Error()
	if adapterError.RetryAfter > 0 {
		result.RetryAfterMilliseconds = adapterError.RetryAfter.Milliseconds()
		if result.RetryAfterMilliseconds == 0 {
			result.RetryAfterMilliseconds = 1
		}
	}
	return result
}
