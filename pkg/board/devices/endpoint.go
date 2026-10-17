package devices

import (
	"devicekit"
	"devicekit/device"
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
	return &device.Provider{ProviderID: o.ID, Device: devicekit.Device{ID: o.DeviceID, Kind: "remote", Name: o.Name, Capabilities: []devicekit.CapabilityDescriptor{
		device.Descriptor(devicekit.DeviceHealth, device.EmptySchema, false), device.Descriptor(devicekit.RemoteConnect, device.EmptySchema, false),
	}}, Bind: func(context.Context, devicekit.Grant) (device.Binding, error) {
		return device.Binding{Health: func(ctx context.Context) (bool, error) {
			v, err := networkprobe.New(networkprobe.Config{Dialer: o.Dialer}).Observe(ctx, networkprobe.NewTCPRequest(o.Address))
			return v.Healthy, err
		}, Operations: map[devicekit.Capability]device.Operation{
			devicekit.DeviceHealth: {Invoke: func(ctx context.Context, a devicekit.Action) (devicekit.Result, error) {
				if err := device.Empty(a.Input); err != nil {
					return devicekit.Result{}, err
				}
				v, err := networkprobe.New(networkprobe.Config{Dialer: o.Dialer}).Observe(ctx, networkprobe.NewTCPRequest(o.Address))
				if err != nil {
					return devicekit.Result{}, err
				}
				return device.Result(v)
			}},
			devicekit.RemoteConnect: {Channel: func(ctx context.Context, a devicekit.Action, _ devicekit.ChannelLimits) (io.ReadWriteCloser, error) {
				if err := device.Empty(a.Input); err != nil {
					return nil, err
				}
				return o.Dialer.DialContext(ctx, "tcp", o.Address)
			}},
		}}, nil
	}}, nil
}
