// Package wirebridge provides a small JSON name and version translation for
// executable plugins that support both a host contract and a native contract.
package wirebridge

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

type Mapping struct {
	PublicPrefix   string
	InternalPrefix string
	ToInternal     map[string]string
	ToPublic       map[string]string
	KeysToInternal map[string]string
	KeysToPublic   map[string]string
}

func (mapping Mapping) NormalizeMethod(method string) (string, bool, error) {
	if strings.HasPrefix(method, mapping.PublicPrefix) {
		return mapping.InternalPrefix + strings.TrimPrefix(method, mapping.PublicPrefix), true, nil
	}
	if strings.HasPrefix(method, mapping.InternalPrefix) || method == "plugin.hello" {
		return method, false, nil
	}
	return "", false, fmt.Errorf("unsupported method %q", method)
}

func (mapping Mapping) translate(value any, stringsMap, keys map[string]string) any {
	switch value := value.(type) {
	case []any:
		for index := range value {
			value[index] = mapping.translate(value[index], stringsMap, keys)
		}
		return value
	case map[string]any:
		result := make(map[string]any, len(value))
		for key, item := range value {
			if replacement, ok := keys[key]; ok {
				key = replacement
			}
			result[key] = mapping.translate(item, stringsMap, keys)
		}
		return result
	case string:
		if replacement, ok := stringsMap[value]; ok {
			return replacement
		}
	}
	return value
}

func (mapping Mapping) TranslateParams(raw json.RawMessage, public bool) (json.RawMessage, error) {
	if !public || len(raw) == 0 || string(raw) == "null" {
		return raw, nil
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, err
	}
	return json.Marshal(mapping.translate(value, mapping.ToInternal, mapping.KeysToInternal))
}

func (mapping Mapping) TranslateResult(value any, public bool) (any, error) {
	if !public || value == nil {
		return value, nil
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var generic any
	if err := json.Unmarshal(encoded, &generic); err != nil {
		return nil, err
	}
	return mapping.translate(generic, mapping.ToPublic, mapping.KeysToPublic), nil
}

func (mapping Mapping) WrapCall(method string, raw json.RawMessage, call func(string, json.RawMessage) (any, error)) (any, error) {
	normalized, public, err := mapping.NormalizeMethod(method)
	if err != nil {
		return nil, err
	}
	params, err := mapping.TranslateParams(raw, public)
	if err != nil {
		return nil, errors.New("decode public plugin request")
	}
	result, err := call(normalized, params)
	if err != nil {
		return nil, err
	}
	return mapping.TranslateResult(result, public)
}
