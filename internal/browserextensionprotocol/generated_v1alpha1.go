// Code generated from protocol/browser-extension/v1alpha1/protocol.schema.json; DO NOT EDIT.
// Schema SHA-256: 700967cedccd9c45dd1492b6bcc10903b9882d8ce47c1b4783544450448f3d24

package browserextensionprotocol

import (
	"context"
	"encoding/json"
)

const ProtocolVersion = "jangolova.browser-extension/v1alpha1"

type CallType string

const (
	CallTypeJangolova CallType = "JANGOLOVA_EXTENSION_CALL"
	CallTypeCymonkey  CallType = "CYMONKEY_CALL"
)

type Method string

const (
	MethodHello                     Method = "hello"
	MethodCapabilities              Method = "capabilities"
	MethodDescribe                  Method = "describe"
	MethodEvents                    Method = "events"
	MethodCymonkeyCall              Method = "cymonkey.call"
	MethodCymonkeyEngineCall         Method = "cymonkey-engine.call"
	MethodPolicyDescribe            Method = "policy.describe"
	MethodPolicyReplace             Method = "policy.replace"
	MethodControlWebsocketDescribe  Method = "control.websocket.describe"
	MethodControlWebsocketConfigure Method = "control.websocket.configure"
	MethodControlWebsocketDisable   Method = "control.websocket.disable"
	MethodAct                       Method = "act"
)

type ControlCall struct {
	Type   CallType       `json:"type"`
	ID     any            `json:"id,omitempty"`
	Method Method         `json:"method"`
	Params map[string]any `json:"params,omitempty"`
}

type ControlPolicyRule struct {
	ID              string   `json:"id"`
	Decision        string   `json:"decision"`
	Callers         []string `json:"callers,omitempty"`
	Capabilities    []string `json:"capabilities,omitempty"`
	Effects         []string `json:"effects,omitempty"`
	Origins         []string `json:"origins,omitempty"`
	TabIDs          []int    `json:"tabIds,omitempty"`
	AugmentationIDs []string `json:"augmentationIds,omitempty"`
}

type ControlPolicy struct {
	Version         int                 `json:"version"`
	DefaultDecision string              `json:"defaultDecision"`
	Rules           []ControlPolicyRule `json:"rules"`
}

type OutboundControlConfiguration struct {
	Endpoint  string `json:"endpoint"`
	Token     string `json:"token"`
	ExpiresAt string `json:"expiresAt"`
}

type AuthRequest struct {
	Type            string `json:"type"`
	ProtocolVersion string `json:"protocolVersion"`
	Token           string `json:"token"`
}

type ControlResponse struct {
	Type   string          `json:"type"`
	ID     any             `json:"id,omitempty"`
	OK     bool            `json:"ok"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  string          `json:"error,omitempty"`
}

type Transport interface {
	Call(context.Context, ControlCall) (ControlResponse, error)
}

type Client struct {
	Transport Transport
}

func (c Client) Call(ctx context.Context, callType CallType, method Method, params map[string]any) (json.RawMessage, error) {
	response, err := c.Transport.Call(ctx, ControlCall{Type: callType, Method: method, Params: params})
	if err != nil {
		return nil, err
	}
	if !response.OK {
		return nil, &ControlError{Message: response.Error}
	}
	return response.Result, nil
}

type ControlError struct {
	Message string
}

func (e *ControlError) Error() string {
	if e.Message == "" {
		return "browser-extension control call failed"
	}
	return e.Message
}
