// A deliberately small executable provider used by integration tests.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/webong/ext/pkg/plugin-go/contract/host/store"
	packagekit "github.com/webong/ext/pkg/plugin-go/contract/package"
	"github.com/webong/ext/pkg/plugin-go/contract/transport/jsonline"
)

type answer struct {
	Result any
	Error  string
}

func main() {
	descriptor, err := packagekit.LoadDescriptor(filepath.Join(filepath.Dir(os.Args[0]), store.ManifestName))
	if err != nil {
		os.Exit(2)
	}
	name := descriptor.Identity.ID
	started := false
	err = jsonline.ServeFunc(context.Background(), descriptor, func(_ context.Context, method string, payload json.RawMessage) (any, error) {
		adapterAPI := "content.adapter/v1"
		observationAPI := "content.observation/v1"
		contentAPI := "content.item/v1"
		var response answer
		switch method {
		case "display.engine.inspect":
			response.Result = map[string]any{"Available": true, "Capabilities": []string{"window.screenshot"}}
		case "display.engine.connect":
			response.Result = map[string]any{"capabilities": []string{"window.screenshot"}}
		case "display.engine.authorize":
			response.Result = map[string]any{"Authorized": true}
		case "display.engine.call":
			response.Result = map[string]any{"ok": true}
		case "display.engine.health":
			response.Result = map[string]any{"Status": "connected"}
		case "display.engine.events":
			response.Result = map[string]any{"cursor": "0", "events": []any{}}
		case "display.engine.material", "display.engine.disconnect":
			response.Result = map[string]any{}
		case "content.provider.start":
			started = true
			response.Result = map[string]any{}
		case "content.provider.close":
			started = false
			response.Result = map[string]any{}
		case "content.provider.health":
			response.Result = map[string]any{"APIVersion": adapterAPI, "Ready": true}
		case "content.provider.capabilities":
			capabilities := []string{"image.observe", "text.interpret", "audio.interpret", "multimodal.interpret"}
			if name == "content-fixture" {
				capabilities = []string{"text.interpret", "audio.interpret"}
			}
			response.Result = map[string]any{"APIVersion": adapterAPI, "Capabilities": capabilities}
		case "content.provider.observe":
			var params struct {
				Request struct {
					APIVersion string `json:"apiVersion"`
					Request    struct {
						APIVersion string `json:"apiVersion"`
						RequestID  string `json:"requestId"`
					} `json:"request"`
				} `json:"request"`
			}
			_ = json.Unmarshal(payload, &params)
			if params.Request.APIVersion != "content.adapter/v1" || params.Request.Request.APIVersion != "content.observation/v1" {
				response.Error = "observation request used an unexpected apiVersion"
				break
			}
			response.Result = map[string]any{"apiVersion": adapterAPI, "response": map[string]any{"apiVersion": observationAPI, "requestId": params.Request.Request.RequestID, "observations": []any{}}}
		case "content.provider.interpret":
			var params struct {
				Request struct {
					Request struct {
						RequestID string `json:"requestId"`
						Parts     []struct {
							Kind string `json:"kind"`
						} `json:"parts"`
					} `json:"request"`
				} `json:"request"`
			}
			_ = json.Unmarshal(payload, &params)
			kind := "summary"
			if len(params.Request.Request.Parts) > 0 && params.Request.Request.Parts[0].Kind == "audio" {
				kind = "transcript"
			}
			response.Result = map[string]any{"apiVersion": adapterAPI, "response": map[string]any{"apiVersion": contentAPI, "requestId": params.Request.Request.RequestID, "findings": []any{map[string]any{"kind": kind, "text": "fixture interpretation", "sourceParts": []int{0}}}}}
		case "content.provider.model.inspect":
			if started {
				response.Error = "model inspection must happen before provider startup"
			} else {
				response.Result = map[string]any{"apiVersion": adapterAPI, "compatible": true}
			}
		case "device.provider.list":
			response.Result = []any{map[string]any{"id": "device", "kind": "drive", "capabilities": []any{map[string]any{"name": "drive.read", "inputSchema": map[string]any{"type": "object"}}}}}
		case "device.provider.open":
			response.Result = map[string]any{}
		case "device.provider.invoke":
			response.Result = map[string]any{"output": map[string]any{"ok": true}}
		case "device.provider.stream.open":
			response.Result = map[string]any{"id": "stream", "size": 7, "truncated": false}
		case "device.provider.stream.read":
			response.Result = map[string]any{"data": []byte("content"), "eof": true}
		case "device.provider.stream.close", "device.provider.close":
			response.Result = map[string]any{}
		default:
			response.Error = "unknown fixture method"
		}
		if response.Error != "" {
			return nil, errors.New(response.Error)
		}
		return response.Result, nil
	}, jsonline.FuncOptions{})
	if err != nil {
		os.Exit(1)
	}
}
