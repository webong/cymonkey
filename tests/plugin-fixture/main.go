// A deliberately small executable provider used by integration tests.
package main

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

type request struct {
	ID     uint64          `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}
type response struct {
	ID     uint64 `json:"id"`
	Result any    `json:"result,omitempty"`
	Error  string `json:"error,omitempty"`
}

func main() {
	manifestBytes, err := os.ReadFile(filepath.Join(filepath.Dir(os.Args[0]), "plugin.json"))
	if err != nil {
		os.Exit(2)
	}
	var manifest struct {
		APIVersion string `json:"apiVersion"`
		Name       string `json:"name"`
		Version    string `json:"version"`
		Kind       string `json:"kind,omitempty"`
		Contracts  []struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"contracts,omitempty"`
	}
	if json.Unmarshal(manifestBytes, &manifest) != nil {
		os.Exit(2)
	}
	reader := bufio.NewScanner(os.Stdin)
	reader.Buffer(make([]byte, 4096), 24<<20)
	writer := bufio.NewWriter(os.Stdout)
	started := false
	publicContract := ""
	for _, contract := range manifest.Contracts {
		if strings.HasPrefix(contract.Name, "cymonkey.") {
			publicContract = contract.Name + "."
			break
		}
	}
	if publicContract == "" && strings.HasPrefix(manifest.Kind, "cymonkey.") {
		publicContract = manifest.Kind + "."
	}
	for reader.Scan() {
		var call request
		if json.Unmarshal(reader.Bytes(), &call) != nil {
			os.Exit(2)
		}
		answer := response{ID: call.ID}
		if call.Method != "plugin.hello" && publicContract != "" && !strings.HasPrefix(call.Method, publicContract) {
			answer.Error = "public plugin received an internal library method"
			encoded, _ := json.Marshal(answer)
			_, _ = writer.Write(append(encoded, '\n'))
			_ = writer.Flush()
			continue
		}
		method := call.Method
		publicContent := strings.HasPrefix(method, "cymonkey.content.")
		method = strings.NewReplacer(
			"cymonkey.display.", "jangolova.",
			"cymonkey.content.", "blockade.",
			"cymonkey.device.", "board.",
		).Replace(method)
		adapterAPI := "blockade.provider-adapter/v1alpha1"
		observationAPI := "blockade.observation/v1alpha1"
		contentAPI := "blockade.content/v1alpha1"
		if publicContent {
			adapterAPI = "cymonkey.content-adapter/v1alpha1"
			observationAPI = "cymonkey.observation-result/v1alpha1"
			contentAPI = "cymonkey.content/v1alpha1"
		}
		switch method {
		case "plugin.hello":
			answer.Result = manifest
		case "jangolova.inspect":
			answer.Result = map[string]any{"Available": true, "Capabilities": []string{"window.screenshot"}}
		case "jangolova.connect":
			answer.Result = map[string]any{"capabilities": []string{"window.screenshot"}}
		case "jangolova.authorize":
			answer.Result = map[string]any{"Authorized": true}
		case "jangolova.call":
			answer.Result = map[string]any{"ok": true}
		case "jangolova.health":
			answer.Result = map[string]any{"Status": "connected"}
		case "jangolova.events":
			answer.Result = map[string]any{"cursor": "0", "events": []any{}}
		case "jangolova.material", "jangolova.disconnect":
			answer.Result = map[string]any{}
		case "blockade.start":
			started = true
			answer.Result = map[string]any{}
		case "blockade.close":
			started = false
			answer.Result = map[string]any{}
		case "blockade.health":
			answer.Result = map[string]any{"APIVersion": adapterAPI, "Ready": true}
		case "blockade.capabilities":
			capabilities := []string{"image.observe", "text.interpret", "audio.interpret", "multimodal.interpret"}
			if manifest.Name == "content-fixture" {
				capabilities = []string{"text.interpret", "audio.interpret"}
			} else if manifest.Name == "legacy-vision-fixture" {
				capabilities = []string{"image.describe"}
			}
			answer.Result = map[string]any{"APIVersion": adapterAPI, "Capabilities": capabilities}
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
			_ = json.Unmarshal(call.Params, &params)
			if publicContent && (params.Request.APIVersion != "cymonkey.content-adapter/v1alpha1" || params.Request.Request.APIVersion != "cymonkey.observation/v1alpha1") {
				answer.Error = "public observation request used an internal contract"
				break
			}
			answer.Result = map[string]any{"apiVersion": adapterAPI, "response": map[string]any{"apiVersion": observationAPI, "requestId": params.Request.Request.RequestID, "observations": []any{}}}
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
			_ = json.Unmarshal(call.Params, &params)
			kind := "summary"
			if len(params.Request.Request.Parts) > 0 && params.Request.Request.Parts[0].Kind == "audio" {
				kind = "transcript"
			}
			answer.Result = map[string]any{"apiVersion": adapterAPI, "response": map[string]any{"apiVersion": contentAPI, "requestId": params.Request.Request.RequestID, "findings": []any{map[string]any{"kind": kind, "text": "fixture interpretation", "sourceParts": []int{0}}}}}
		case "blockade.model.inspect":
			if started {
				answer.Error = "model inspection must happen before provider startup"
			} else {
				answer.Result = map[string]any{"apiVersion": adapterAPI, "compatible": true}
			}
		case "board.list":
			answer.Result = []any{map[string]any{"id": "device", "kind": "drive", "capabilities": []any{map[string]any{"name": "drive.read", "inputSchema": map[string]any{"type": "object"}}}}}
		case "board.open":
			answer.Result = map[string]any{}
		case "board.invoke":
			answer.Result = map[string]any{"output": map[string]any{"ok": true}}
		case "board.stream.open":
			answer.Result = map[string]any{"id": "stream", "size": 7, "truncated": false}
		case "board.stream.read":
			answer.Result = map[string]any{"data": []byte("content"), "eof": true}
		case "board.stream.close", "board.close":
			answer.Result = map[string]any{}
		default:
			answer.Error = "unknown fixture method"
		}
		encoded, _ := json.Marshal(answer)
		_, _ = writer.Write(append(encoded, '\n'))
		_ = writer.Flush()
	}
}
