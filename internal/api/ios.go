//go:build !android

package api

import (
	"context"

	"github.com/swipt-automation/swipt/internal/device"
	"github.com/swipt-automation/swipt/internal/proto"
)

type connectedDevice struct {
	*device.Connection
}

func NewDevice(connection *device.Connection) Device {
	return connectedDevice{connection}
}

func (d connectedDevice) Identifier() string {
	return d.DeviceInfo().UDID
}

func (d connectedDevice) EnableJIT(ctx context.Context, bundleID string, pid uint64) error {
	return d.Phone().EnableJIT(ctx, bundleID, pid)
}

func (d connectedDevice) Status() *proto.DeviceConnection {
	return deviceConnection(d.State())
}

func deviceConnection(state device.State, err error) *proto.DeviceConnection {
	out := &proto.DeviceConnection{}
	switch state {
	case device.StateStopped:
		out.State = proto.DeviceConnectionState_DEVICE_CONNECTION_STATE_STOPPED
	case device.StateConnecting:
		out.State = proto.DeviceConnectionState_DEVICE_CONNECTION_STATE_CONNECTING
	case device.StatePreparing:
		out.State = proto.DeviceConnectionState_DEVICE_CONNECTION_STATE_PREPARING
	case device.StateRunning:
		out.State = proto.DeviceConnectionState_DEVICE_CONNECTION_STATE_RUNNING
	case device.StateFailed:
		out.State = proto.DeviceConnectionState_DEVICE_CONNECTION_STATE_FAILED
	}
	if err == nil {
		return out
	}
	out.Message = err.Error()
	switch device.ProblemOf(err) {
	case device.ProblemNoTunnel:
		out.Problem = proto.DeviceConnectionProblem_DEVICE_CONNECTION_PROBLEM_UNREACHABLE
	case device.ProblemPairingRejected:
		out.Problem = proto.DeviceConnectionProblem_DEVICE_CONNECTION_PROBLEM_PAIRING_REJECTED
	case device.ProblemDeveloperModeOff:
		out.Problem = proto.DeviceConnectionProblem_DEVICE_CONNECTION_PROBLEM_DEVELOPER_MODE_OFF
	case device.ProblemNoDiskImage:
		out.Problem = proto.DeviceConnectionProblem_DEVICE_CONNECTION_PROBLEM_DISK_IMAGE_MISSING
	case device.ProblemDiskImage:
		out.Problem = proto.DeviceConnectionProblem_DEVICE_CONNECTION_PROBLEM_DISK_IMAGE_MOUNT_FAILED
	case device.ProblemUnsupported:
		out.Problem = proto.DeviceConnectionProblem_DEVICE_CONNECTION_PROBLEM_DEVICE_UNSUPPORTED
	case device.ProblemScreenInUse:
		out.Problem = proto.DeviceConnectionProblem_DEVICE_CONNECTION_PROBLEM_SCREEN_IN_USE
	case device.ProblemLoopbackAddress:
		out.Problem = proto.DeviceConnectionProblem_DEVICE_CONNECTION_PROBLEM_VPN_PEER_ADDRESS_MISSING
	default:
		out.Problem = proto.DeviceConnectionProblem_DEVICE_CONNECTION_PROBLEM_OTHER
	}
	return out
}
