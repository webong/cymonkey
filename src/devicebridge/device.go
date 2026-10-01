// Package devicebridge translates Cymonkey's public device contract to the
// internal device library contract.
package devicebridge

import (
	"board"
	"cymonkey/src/contracts"
)

func ToLibraryOpen(request contracts.DeviceOpenRequest) board.OpenRequest {
	capabilities := make([]board.Capability, len(request.Grant.Capabilities))
	for index, capability := range request.Grant.Capabilities {
		capabilities[index] = board.Capability(capability)
	}
	return board.OpenRequest{
		ProviderID: request.AdapterID,
		DeviceID:   request.DeviceID,
		Grant: board.Grant{
			Capabilities: capabilities,
			ResourceIDs:  append([]string(nil), request.Grant.ResourceIDs...),
		},
	}
}

func ToLibraryAction(action contracts.DeviceAction) board.Action {
	return board.Action{
		Capability: board.Capability(action.Capability),
		ResourceID: action.ResourceID,
		Input:      action.Input,
	}
}

func FromLibraryDevices(devices []board.Device) contracts.DeviceList {
	result := contracts.DeviceList{APIVersion: contracts.DeviceAPIVersion, Devices: make([]contracts.Device, len(devices))}
	for index, device := range devices {
		capabilities := make([]contracts.DeviceCapability, len(device.Capabilities))
		for capabilityIndex, capability := range device.Capabilities {
			capabilities[capabilityIndex] = contracts.DeviceCapability{
				Name: string(capability.Name), InputSchema: capability.InputSchema,
				OutputSchema: capability.OutputSchema, ResourceRequired: capability.ResourceRequired,
			}
		}
		result.Devices[index] = contracts.Device{
			AdapterID: device.ProviderID, ID: device.ID, Kind: device.Kind,
			Name: device.Name, Capabilities: capabilities,
		}
	}
	return result
}

func FromLibraryResult(result board.Result) contracts.DeviceResult {
	return contracts.DeviceResult{APIVersion: contracts.DeviceAPIVersion, Output: result.Output}
}

func FromLibraryEvents(batch board.EventBatch) contracts.DeviceEventBatch {
	result := contracts.DeviceEventBatch{APIVersion: contracts.DeviceAPIVersion, Cursor: batch.Cursor, Events: make([]contracts.DeviceEvent, len(batch.Events))}
	for index, event := range batch.Events {
		result.Events[index] = contracts.DeviceEvent{Cursor: event.Cursor, Type: event.Type, Capability: string(event.Capability), At: event.At}
	}
	return result
}
