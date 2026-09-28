// A deliberately small executable provider used by integration tests.
package main

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
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
		Kind       string `json:"kind"`
	}
	if json.Unmarshal(manifestBytes, &manifest) != nil {
		os.Exit(2)
	}
	reader := bufio.NewScanner(os.Stdin)
	reader.Buffer(make([]byte, 4096), 24<<20)
	writer := bufio.NewWriter(os.Stdout)
	for reader.Scan() {
		var call request
		if json.Unmarshal(reader.Bytes(), &call) != nil {
			os.Exit(2)
		}
		answer := response{ID: call.ID}
		switch call.Method {
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
		case "blockade.start", "blockade.close":
			answer.Result = map[string]any{}
		case "blockade.health":
			answer.Result = map[string]any{"APIVersion": "blockade.provider-adapter/v1alpha1", "Ready": true}
		case "blockade.capabilities":
			answer.Result = map[string]any{"APIVersion": "blockade.provider-adapter/v1alpha1", "Capabilities": []string{"image.observe"}}
		case "blockade.observe":
			var params struct {
				Request struct {
					Request struct {
						RequestID string `json:"requestId"`
					} `json:"request"`
				} `json:"request"`
			}
			_ = json.Unmarshal(call.Params, &params)
			answer.Result = map[string]any{"apiVersion": "blockade.provider-adapter/v1alpha1", "response": map[string]any{"apiVersion": "blockade.observation/v1alpha1", "requestId": params.Request.Request.RequestID, "observations": []any{}}}
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
