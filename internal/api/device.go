package api

import (
	"context"

	"github.com/swipt-automation/swipt/internal/proto"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

type jitDevice interface {
	EnableJIT(ctx context.Context, bundleID string, pid uint64) error
}

type deviceService struct {
	proto.UnimplementedDeviceServiceServer
	*Server
}

func (s *deviceService) EnableJIT(ctx context.Context, request *proto.EnableJITRequest) (*emptypb.Empty, error) {
	device, isJITDevice := s.options.Device.(jitDevice)
	if !isJITDevice {
		return nil, status.Error(codes.Unimplemented, "the device has no JIT to enable")
	}
	if err := device.EnableJIT(ctx, request.BundleId, request.Pid); err != nil {
		return nil, StatusError(err)
	}
	return &emptypb.Empty{}, nil
}
