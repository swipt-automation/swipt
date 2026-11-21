package api

import (
	"context"

	"github.com/swipt-automation/swipt/internal/proto"

	"google.golang.org/protobuf/types/known/emptypb"
)

type systemService struct {
	proto.UnimplementedSystemServiceServer
	*Server
}

func (s *systemService) GetInfo(context.Context, *emptypb.Empty) (*proto.GetInfoResponse, error) {
	return &proto.GetInfoResponse{
		ServerVersion:    serverVersion(),
		DeviceName:       s.options.Device.Name(),
		DeviceId:         s.options.Device.Identifier(),
		DeviceConnection: s.options.Device.Status(),
		AppVersion:       s.options.AppVersion,
		AppBuild:         s.options.AppBuild,
	}, nil
}

func (s *systemService) Shutdown(ctx context.Context, _ *emptypb.Empty) (*emptypb.Empty, error) {
	if s.options.Shutdown != nil {
		s.options.Shutdown(ctx)
	}
	return &emptypb.Empty{}, nil
}
