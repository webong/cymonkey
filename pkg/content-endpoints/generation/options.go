package generation

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/webong/ext/pkg/plugin-go/contract/validate/jsonschema"
	"jangolova/generationkit"
)

// Native options use the selected API dialect's names. Only documented fields
// are accepted; options cannot replace model, messages, tools or endpoint data.
func applyOptions(protocol Protocol, r generationkit.GenerationRequest, payload map[string]any) error {
	if len(r.Options) == 0 {
		return nil
	}
	var properties string
	switch protocol {
	case OpenAI:
		properties = `"top_p":{"type":"number","minimum":0,"maximum":1},"seed":{"type":"integer"},"frequency_penalty":{"type":"number","minimum":-2,"maximum":2},"presence_penalty":{"type":"number","minimum":-2,"maximum":2},"parallel_tool_calls":{"type":"boolean"},"reasoning_effort":{"type":"string","minLength":1},"tool_choice":{"type":"string","enum":["auto","none","required"]}`
	case Responses:
		properties = `"top_p":{"type":"number","minimum":0,"maximum":1},"parallel_tool_calls":{"type":"boolean"},"tool_choice":{"type":"string","enum":["auto","none","required"]},"reasoning":{"type":"object","properties":{"effort":{"type":"string","minLength":1},"summary":{"type":"string","enum":["auto","concise","detailed"]}},"additionalProperties":false},"text":{"type":"object","properties":{"verbosity":{"type":"string","enum":["low","medium","high"]}},"additionalProperties":false}`
	case Anthropic:
		properties = `"top_p":{"type":"number","minimum":0,"maximum":1},"top_k":{"type":"integer","minimum":0},"stop_sequences":{"type":"array","maxItems":64,"items":{"type":"string","maxLength":1024}},"thinking":{"type":"object","properties":{"type":{"type":"string","enum":["disabled","enabled","adaptive"]},"budget_tokens":{"type":"integer","minimum":1024}},"required":["type"],"additionalProperties":false}`
	case Gemini:
		properties = `"topP":{"type":"number","minimum":0,"maximum":1},"topK":{"type":"integer","minimum":1},"seed":{"type":"integer"},"stopSequences":{"type":"array","maxItems":64,"items":{"type":"string","maxLength":1024}},"thinkingConfig":{"type":"object","properties":{"thinkingBudget":{"type":"integer","minimum":-1},"includeThoughts":{"type":"boolean"},"thinkingLevel":{"type":"string","minLength":1}},"additionalProperties":false}`
	default:
		return unsupported("native options for this protocol")
	}
	schema := json.RawMessage(`{"type":"object","properties":{` + properties + `},"additionalProperties":false}`)
	if err := jsonschema.Validate(schema, r.Options); err != nil {
		return fmt.Errorf("invalid adapter options: %w", err)
	}
	var options map[string]any
	decoder := json.NewDecoder(bytes.NewReader(r.Options))
	decoder.UseNumber()
	if err := decoder.Decode(&options); err != nil {
		return err
	}
	if protocol == Anthropic {
		if thinking, ok := options["thinking"].(map[string]any); ok {
			budgetValue, exists := thinking["budget_tokens"].(json.Number)
			budget, budgetErr := budgetValue.Int64()
			if thinking["type"] == "enabled" {
				limit := r.MaxOutputTokens
				if limit == 0 {
					limit = 4096
				}
				if !exists || budgetErr != nil || budget >= int64(limit) {
					return fmt.Errorf("thinking budget must be less than max output tokens")
				}
			} else if exists {
				return fmt.Errorf("thinking budget requires enabled thinking")
			}
			if thinking["type"] != "disabled" && r.Temperature != nil {
				return unsupported("temperature with thinking")
			}
		}
	}
	if protocol == Gemini {
		config, _ := payload["generationConfig"].(map[string]any)
		if config == nil {
			config = map[string]any{}
		}
		for key, value := range options {
			config[key] = value
		}
		payload["generationConfig"] = config
	} else {
		for key, value := range options {
			if key == "text" {
				existing, _ := payload[key].(map[string]any)
				if existing == nil {
					existing = map[string]any{}
				}
				for k, v := range value.(map[string]any) {
					existing[k] = v
				}
				payload[key] = existing
			} else {
				payload[key] = value
			}
		}
	}
	return nil
}
