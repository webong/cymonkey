package contracts

import (
	"encoding/json"
	"time"
)

const DeviceAPIVersion = "cymonkey.device/v1alpha1"

type DeviceCapability struct {
	Name             string          `json:"name"`
	InputSchema      json.RawMessage `json:"inputSchema"`
	OutputSchema     json.RawMessage `json:"outputSchema,omitempty"`
	ResourceRequired bool            `json:"resourceRequired,omitempty"`
}

type Device struct {
	AdapterID    string             `json:"adapterId"`
	ID           string             `json:"id"`
	Kind         string             `json:"kind"`
	Name         string             `json:"name,omitempty"`
	Capabilities []DeviceCapability `json:"capabilities"`
}

type DeviceList struct {
	APIVersion string   `json:"apiVersion"`
	Devices    []Device `json:"devices"`
}

type DeviceGrant struct {
	Capabilities []string `json:"capabilities"`
	ResourceIDs  []string `json:"resourceIds,omitempty"`
}

type DeviceOpenRequest struct {
	AdapterID string      `json:"adapterId"`
	DeviceID  string      `json:"deviceId"`
	Grant     DeviceGrant `json:"grant"`
}

type DeviceAction struct {
	Capability string          `json:"capability"`
	ResourceID string          `json:"resourceId,omitempty"`
	Input      json.RawMessage `json:"input,omitempty"`
}

type DeviceActionRequest struct {
	Open   DeviceOpenRequest `json:"open"`
	Action DeviceAction      `json:"action"`
}

type DeviceResult struct {
	APIVersion string          `json:"apiVersion"`
	Output     json.RawMessage `json:"output,omitempty"`
}

type DeviceEvent struct {
	Cursor     uint64    `json:"cursor"`
	Type       string    `json:"type"`
	Capability string    `json:"capability,omitempty"`
	At         time.Time `json:"at"`
}

type DeviceEventBatch struct {
	APIVersion string        `json:"apiVersion"`
	Events     []DeviceEvent `json:"events"`
	Cursor     uint64        `json:"cursor"`
}
