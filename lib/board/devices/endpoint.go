package devices

import (
	"board"
	"board/device"
	"board/networkprobe"
	"context"
	"errors"
	"io"
	"net"
	"time"
)

type EndpointOptions struct {
	ID, DeviceID, Name, Address string
	Dialer                      networkprobe.TCPDialer
}

// Endpoint attaches to exactly one host-selected TCP endpoint. remote.connect
// grants opaque bidirectional access; it cannot enforce VNC view-only semantics.
func Endpoint(o EndpointOptions) (*device.Provider, error) {
	if o.ID == "" {
		return nil, errors.New("endpoint needs a provider ID")
	}
	if err := networkprobe.NewTCPRequest(o.Address).Validate(); err != nil {
		return nil, err
	}
	if o.DeviceID == "" {
		o.DeviceID = "endpoint"
	}
	if o.Dialer == nil {
		o.Dialer = &net.Dialer{Timeout: 5 * time.Second}
	}
	return &device.Provider{ProviderID: o.ID, Device: board.Device{ID: o.DeviceID, Kind: "remote", Name: o.Name, Capabilities: []board.CapabilityDescriptor{
		device.Descriptor(board.DeviceHealth, device.EmptySchema, false), device.Descriptor(board.RemoteConnect, device.EmptySchema, false),
	}}, Bind: func(context.Context, board.Grant) (device.Binding, error) {
		return device.Binding{Health: func(ctx context.Context) (bool, error) {
			v, err := networkprobe.New(networkprobe.Config{Dialer: o.Dialer}).Observe(ctx, networkprobe.NewTCPRequest(o.Address))
			return v.Healthy, err
		}, Operations: map[board.Capability]device.Operation{
			board.DeviceHealth: {Invoke: func(ctx context.Context, a board.Action) (board.Result, error) {
				if err := device.Empty(a.Input); err != nil {
					return board.Result{}, err
				}
				v, err := networkprobe.New(networkprobe.Config{Dialer: o.Dialer}).Observe(ctx, networkprobe.NewTCPRequest(o.Address))
				if err != nil {
					return board.Result{}, err
				}
				return device.Result(v)
			}},
			board.RemoteConnect: {Channel: func(ctx context.Context, a board.Action, _ board.ChannelLimits) (io.ReadWriteCloser, error) {
				if err := device.Empty(a.Input); err != nil {
					return nil, err
				}
				return o.Dialer.DialContext(ctx, "tcp", o.Address)
			}},
		}}, nil
	}}, nil
}
