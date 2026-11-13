package api

import (
	"context"

	"github.com/swipt-automation/swipt/internal/device"
	"github.com/swipt-automation/swipt/internal/proto"

	"google.golang.org/protobuf/types/known/emptypb"
)

type systemService struct {
	proto.UnimplementedSystemServiceServer
	*Server
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

func (s *systemService) GetInfo(context.Context, *emptypb.Empty) (*proto.GetInfoResponse, error) {
	return &proto.GetInfoResponse{
		ServerVersion:    serverVersion(),
		DeviceName:       s.options.Device.Name(),
		Udid:             s.options.Device.DeviceInfo().UDID,
		DeviceConnection: deviceConnection(s.options.Device.State()),
		AppVersion:       s.options.AppVersion,
		AppBuild:         s.options.AppBuild,
	}, nil
}

func (s *systemService) Shutdown(context.Context, *emptypb.Empty) (*emptypb.Empty, error) {
	s.options.Shutdown()
	return &emptypb.Empty{}, nil
}
