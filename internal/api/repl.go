package api

import (
	"context"

	"github.com/swipt-automation/swipt/internal/automation"
	"github.com/swipt-automation/swipt/internal/automation/declarations"
	"github.com/swipt-automation/swipt/internal/proto"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"
)

type replService struct {
	proto.UnimplementedREPLServiceServer
	*Server
}

func (s *replService) Execute(request *proto.ExecuteRequest, stream grpc.ServerStreamingServer[proto.ExecuteResponse]) error {
	ctx := stream.Context()
	session, err := s.cliSession()
	if err != nil {
		return err
	}
	var sendErr error
	err = session.Execute(ctx, automation.Script{Code: request.Code, Args: request.Args, Program: request.Program}, 0, func(output automation.Output) {
		if sendErr != nil {
			return
		}
		response := &proto.ExecuteResponse{}
		switch output.Kind {
		case automation.OutputImage:
			response.Part = &proto.ExecuteResponse_Image{Image: output.Image}
		case automation.OutputValue:
			response.Part = &proto.ExecuteResponse_Value{Value: &proto.ScriptValue{Json: string(output.JSON), Text: output.Text}}
		default:
			response.Part = &proto.ExecuteResponse_Console{Console: &proto.ConsoleOutput{Level: logLevel(output.Level), Text: output.Text}}
		}
		sendErr = stream.Send(response)
	})
	if sendErr != nil {
		return sendErr
	}
	if revoked := revocation(ctx); revoked != nil {
		return revoked
	}
	if err == nil {
		return nil
	}
	failure, isFailure := automation.ScriptFailure(err)
	if !isFailure {
		return StatusError(err)
	}
	return stream.Send(&proto.ExecuteResponse{Part: &proto.ExecuteResponse_Error{Error: scriptError(failure)}})
}

func (s *replService) Reset(ctx context.Context, _ *emptypb.Empty) (*emptypb.Empty, error) {
	session, err := s.cliSession()
	if err != nil {
		return nil, err
	}
	if err = session.Reset(ctx); err != nil {
		return nil, StatusError(err)
	}
	return &emptypb.Empty{}, nil
}

func (s *replService) GetDeclarations(context.Context, *emptypb.Empty) (*proto.GetDeclarationsResponse, error) {
	return &proto.GetDeclarationsResponse{Typescript: declarations.TypeScript}, nil
}
