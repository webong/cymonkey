// A deliberately small executable provider used by integration tests.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

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
		publicContent := strings.HasPrefix(method, "cymonkey.content.")
		method = strings.NewReplacer(
			"cymonkey.display.", "jangolova.",
			"cymonkey.content.", "blockade.",
			"cymonkey.device.", "board.",
			"jangolova.engine.", "jangolova.",
			"blockade.provider.", "blockade.",
			"board.provider.", "board.",
		).Replace(method)
		adapterAPI := "blockade.provider-adapter/v1alpha1"
		observationAPI := "blockade.observation/v1alpha1"
		contentAPI := "blockade.content/v1alpha1"
		if publicContent {
			adapterAPI = "cymonkey.content-adapter/v1alpha1"
			observationAPI = "cymonkey.observation-result/v1alpha1"
			contentAPI = "cymonkey.content/v1alpha1"
		}
		var response answer
		switch method {
		case "jangolova.inspect":
			response.Result = map[string]any{"Available": true, "Capabilities": []string{"window.screenshot"}}
		case "jangolova.connect":
			response.Result = map[string]any{"capabilities": []string{"window.screenshot"}}
		case "jangolova.authorize":
			response.Result = map[string]any{"Authorized": true}
		case "jangolova.call":
			response.Result = map[string]any{"ok": true}
		case "jangolova.health":
			response.Result = map[string]any{"Status": "connected"}
		case "jangolova.events":
			response.Result = map[string]any{"cursor": "0", "events": []any{}}
		case "jangolova.material", "jangolova.disconnect":
			response.Result = map[string]any{}
		case "blockade.start":
			started = true
			response.Result = map[string]any{}
		case "blockade.close":
			started = false
			response.Result = map[string]any{}
		case "blockade.health":
			response.Result = map[string]any{"APIVersion": adapterAPI, "Ready": true}
		case "blockade.capabilities":
			capabilities := []string{"image.observe", "text.interpret", "audio.interpret", "multimodal.interpret"}
			if name == "content-fixture" {
				capabilities = []string{"text.interpret", "audio.interpret"}
			}
			response.Result = map[string]any{"APIVersion": adapterAPI, "Capabilities": capabilities}
		case "blockade.observe":
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
			if publicContent && (params.Request.APIVersion != "cymonkey.content-adapter/v1alpha1" || params.Request.Request.APIVersion != "cymonkey.observation/v1alpha1") {
				response.Error = "public observation request used an internal contract"
				break
			}
			response.Result = map[string]any{"apiVersion": adapterAPI, "response": map[string]any{"apiVersion": observationAPI, "requestId": params.Request.Request.RequestID, "observations": []any{}}}
		case "blockade.interpret":
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
		case "blockade.model.inspect":
			if started {
				response.Error = "model inspection must happen before provider startup"
			} else {
				response.Result = map[string]any{"apiVersion": adapterAPI, "compatible": true}
			}
		case "board.list":
			response.Result = []any{map[string]any{"id": "device", "kind": "drive", "capabilities": []any{map[string]any{"name": "drive.read", "inputSchema": map[string]any{"type": "object"}}}}}
		case "board.open":
			response.Result = map[string]any{}
		case "board.invoke":
			response.Result = map[string]any{"output": map[string]any{"ok": true}}
		case "board.stream.open":
			response.Result = map[string]any{"id": "stream", "size": 7, "truncated": false}
		case "board.stream.read":
			response.Result = map[string]any{"data": []byte("content"), "eof": true}
		case "board.stream.close", "board.close":
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
