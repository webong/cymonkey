package providerexec

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"jangolova/contentkit"
	"strings"

	plugin "github.com/webong/ext/pkg/plugin-go/contract"
	packagekit "github.com/webong/ext/pkg/plugin-go/contract/package"
	"github.com/webong/ext/pkg/plugin-go/contract/transport/jsonline"
)

type StartRequest struct {
	ID       string                     `json:"id"`
	Settings map[string]string          `json:"settings,omitempty"`
	Model    *contentkit.InstalledModel `json:"model,omitempty"`
}

type Provider interface {
	Start(context.Context, StartRequest) error
	Interpret(context.Context, contentkit.ProviderAdapterInterpretRequest, map[string]string) (contentkit.ProviderAdapterInterpretResponse, error)
	Capabilities(context.Context) (contentkit.ProviderAdapterCapabilities, error)
	Health(context.Context) (contentkit.ProviderAdapterHealth, error)
	Close() error
}

// Serve answers the ext handshake and runs the provider on stdin and stdout.
func Serve(ctx context.Context, descriptor plugin.Descriptor, provider Provider) error {
	manifest := packagekit.Manifest{Descriptor: descriptor}
	if !manifest.Supports("content.provider", "v1") {
		return errors.New("plugin manifest does not declare a supported content contract")
	}
	defer provider.Close()
	return jsonline.ServeFunc(ctx, descriptor, func(ctx context.Context, method string, raw json.RawMessage) (any, error) {
		result, err := call(ctx, provider, method, raw)
		if err != nil {
			return nil, encodeFailure(err)
		}
		return result, nil
	}, jsonline.FuncOptions{})
}

func call(ctx context.Context, provider Provider, method string, raw json.RawMessage) (any, error) {
	switch method {
	case "content.provider.start":
		var params StartRequest
		if err := decodeParams(raw, &params); err != nil {
			return nil, invalidRequest(err)
		}
		if params.Model != nil {
			return nil, invalidRequest(errors.New("endpoint providers do not use Cymonkey-installed models"))
		}
		return map[string]any{}, provider.Start(ctx, params)
	case "content.provider.capabilities":
		return provider.Capabilities(ctx)
	case "content.provider.health":
		return provider.Health(ctx)
	case "content.provider.interpret":
		var params struct {
			Request contentkit.ProviderAdapterInterpretRequest `json:"request"`
			Secrets map[string]string                          `json:"secrets,omitempty"`
		}
		if err := decodeParams(raw, &params); err != nil {
			return nil, invalidRequest(err)
		}
		if params.Request.APIVersion != contentkit.ProviderAdapterAPIVersion {
			return nil, invalidRequest(errors.New("unsupported provider adapter apiVersion"))
		}
		if err := contentkit.ValidateInterpretRequest(params.Request.Request); err != nil {
			return nil, invalidRequest(err)
		}
		return provider.Interpret(ctx, params.Request, params.Secrets)
	case "content.provider.close":
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
	return contentkit.NewProviderAdapterError(contentkit.ProviderAdapterErrorInvalidRequest, err)
}

func encodeFailure(err error) *plugin.RemoteError {
	result := &plugin.RemoteError{Code: string(contentkit.ProviderAdapterErrorUnavailable), Message: "provider failed"}
	var adapterError *contentkit.ProviderAdapterError
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
