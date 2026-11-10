package api

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"path"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/sagernet/sing/common"
	E "github.com/sagernet/sing/common/exceptions"
	F "github.com/sagernet/sing/common/format"
	"github.com/swipt-automation/swipt/internal/automation"
	"github.com/swipt-automation/swipt/internal/device"
	"github.com/swipt-automation/swipt/internal/log"
	"github.com/swipt-automation/swipt/internal/proto"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const largestUpload = 128 << 20

type swipletService struct {
	proto.UnimplementedSwipletServiceServer
	*Server
}

func schemaOf(data json.RawMessage) *structpb.Struct {
	if len(data) == 0 {
		return nil
	}
	schema := &structpb.Struct{}
	if protojson.Unmarshal(data, schema) != nil {
		return nil
	}
	return schema
}

func serviceStateOf(state automation.ServiceState) *proto.ServiceState {
	out := &proto.ServiceState{Enabled: state.Enabled}
	if state.Failure != nil {
		out.Error = scriptError(state.Failure)
	}
	switch state.Status {
	case automation.ServiceStopped:
		out.Status = proto.ServiceStatus_SERVICE_STATUS_STOPPED
	case automation.ServiceStarting:
		out.Status = proto.ServiceStatus_SERVICE_STATUS_STARTING
	case automation.ServiceRunning:
		out.Status = proto.ServiceStatus_SERVICE_STATUS_RUNNING
	case automation.ServiceFailed:
		out.Status = proto.ServiceStatus_SERVICE_STATUS_FAILED
	}
	if !state.Since.IsZero() {
		out.Since = timestamppb.New(state.Since)
	}
	return out
}

func (s *swipletService) swipletOf(swiplet automation.Swiplet) *proto.Swiplet {
	manifest := swiplet.Manifest
	out := &proto.Swiplet{
		Id: swiplet.ID, Title: manifest.Name, Version: manifest.Version, HasService: manifest.Service,
		Actions: common.Map(manifest.Actions, func(action automation.ActionInfo) *proto.SwipletAction {
			return &proto.SwipletAction{
				Name: action.Name, Title: action.Title, Description: action.Description,
				InputSchema: schemaOf(action.Input), OutputSchema: schemaOf(action.Output),
			}
		}),
	}
	if manifest.Service {
		if state, err := s.options.Swiplets.Service(swiplet.ID); err == nil {
			out.Service = serviceStateOf(state)
		}
	}
	return out
}

func (s *swipletService) ListSwiplets(context.Context, *emptypb.Empty) (*proto.ListSwipletsResponse, error) {
	swiplets, err := s.options.Swiplets.Swiplets()
	if err != nil {
		return nil, apiError(err)
	}
	return &proto.ListSwipletsResponse{Swiplets: common.Map(swiplets, s.swipletOf)}, nil
}

func inputOf(value *structpb.Value) (json.RawMessage, error) {
	if value == nil {
		return nil, nil
	}
	data, err := protojson.Marshal(value)
	if err != nil {
		return nil, E.Cause1(device.ErrInvalid, err)
	}
	return data, nil
}

func logOf(tag string, entry automation.LogEntry) *proto.LogEntry {
	return logEntry(log.Entry{Time: entry.Time, Level: entry.Level, Tag: tag, Message: entry.Message})
}

func scriptError(failure *automation.Failure) *proto.ScriptError {
	return &proto.ScriptError{
		Name: failure.Name, Message: failure.Message,
		Stack: common.Map(failure.Stack, func(frame automation.Frame) *proto.StackFrame {
			return &proto.StackFrame{Function: frame.Function, File: frame.File, Line: uint32(max(frame.Line, 0)), Column: uint32(max(frame.Column, 0))}
		}),
	}
}

func actionResult(output json.RawMessage, err error) (*proto.ActionResult, error) {
	if failure, isFailure := automation.ScriptFailure(err); isFailure {
		return &proto.ActionResult{Outcome: &proto.ActionResult_Error{Error: scriptError(failure)}}, nil
	}
	if err != nil {
		return nil, apiError(err)
	}
	value := &structpb.Value{}
	if err = protojson.Unmarshal(output, value); err != nil {
		return nil, apiError(E.Cause(err, "read the output"))
	}
	return &proto.ActionResult{Outcome: &proto.ActionResult_Output{Output: value}}, nil
}

type execution struct {
	output json.RawMessage
	err    error
}

func (s *swipletService) CallAction(request *proto.CallActionRequest, stream grpc.ServerStreamingServer[proto.CallActionResponse]) error {
	ctx, cancel := context.WithCancelCause(stream.Context())
	defer cancel(nil)
	input, err := inputOf(request.Input)
	if err != nil {
		return apiError(err)
	}
	tag := "swiplet/" + request.SwipletId
	parts := make(chan *proto.CallActionResponse, logBuffer)
	done := make(chan execution, 1)
	go func() {
		output, err := s.options.Swiplets.Call(ctx, automation.CallRequest{
			Swiplet: request.SwipletId, Action: request.Action, Input: input, Caller: automation.CallerAPI,
			Dialog: s.options.Dialog,
			Log: func(entry automation.LogEntry) {
				select {
				case parts <- &proto.CallActionResponse{Part: &proto.CallActionResponse_Log{Log: logOf(tag, entry)}}:
				default:
				}
			},
			Emit: func(data json.RawMessage) {
				value := &structpb.Value{}
				if err := protojson.Unmarshal(data, value); err != nil {
					cancel(E.Cause(err, "read the value"))
					return
				}
				select {
				case parts <- &proto.CallActionResponse{Part: &proto.CallActionResponse_Value{Value: value}}:
				case <-ctx.Done():
				}
			},
		})
		done <- execution{output, err}
	}()
	var result execution
	for finished := false; !finished; {
		select {
		case part := <-parts:
			if err = stream.Send(part); err != nil {
				cancel(err)
				<-done
				return err
			}
		case result = <-done:
			finished = true
		}
	}
	for len(parts) > 0 {
		if err = stream.Send(<-parts); err != nil {
			return err
		}
	}
	if revoked := revocation(stream.Context()); revoked != nil {
		return revoked
	}
	if cause := context.Cause(ctx); cause != nil && stream.Context().Err() == nil {
		return apiError(cause)
	}
	if result.err == nil && len(result.output) == 0 {
		return stream.Send(&proto.CallActionResponse{Part: &proto.CallActionResponse_Result{Result: &proto.ActionResult{}}})
	}
	actionResult, err := actionResult(result.output, result.err)
	if err != nil {
		return err
	}
	return stream.Send(&proto.CallActionResponse{Part: &proto.CallActionResponse_Result{Result: actionResult}})
}

func (s *swipletService) SetServiceEnabled(ctx context.Context, request *proto.SetServiceEnabledRequest) (*proto.SetServiceEnabledResponse, error) {
	host := s.options.Swiplets
	requested := time.Now()
	if err := host.SetServiceEnabled(request.SwipletId, request.Enabled); err != nil {
		return nil, apiError(err)
	}
	state, err := host.AwaitService(ctx, request.SwipletId, request.Enabled, requested)
	if err != nil {
		return nil, apiError(err)
	}
	return &proto.SetServiceEnabledResponse{Service: serviceStateOf(state)}, nil
}

func (s *swipletService) WatchServices(request *proto.WatchServicesRequest, stream grpc.ServerStreamingServer[proto.WatchServicesResponse]) error {
	if id := request.SwipletId; id != "" {
		if _, err := s.options.Swiplets.Service(id); err != nil {
			return apiError(err)
		}
	}
	ctx, cancel := context.WithCancelCause(stream.Context())
	defer cancel(nil)
	err := s.options.Swiplets.WatchServices(ctx, func(id string, state automation.ServiceState) {
		if request.SwipletId != "" && id != request.SwipletId {
			return
		}
		if err := stream.Send(&proto.WatchServicesResponse{SwipletId: id, Service: serviceStateOf(state)}); err != nil {
			cancel(err)
		}
	})
	if cause := context.Cause(ctx); cause != nil && !errors.Is(cause, context.Canceled) {
		return cause
	}
	return apiError(err)
}

func (s *swipletService) DeploySwiplet(stream grpc.ClientStreamingServer[proto.DeploySwipletRequest, proto.DeploySwipletResponse]) error {
	var archive bytes.Buffer
	for {
		request, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		if archive.Len()+len(request.Chunk) > largestUpload {
			return status.Error(codes.ResourceExhausted, F.ToString("the archive is larger than ", largestUpload>>20, " MiB"))
		}
		archive.Write(request.Chunk)
	}
	reader, err := zip.NewReader(bytes.NewReader(archive.Bytes()), int64(archive.Len()))
	if err != nil {
		return apiError(E.Cause1(device.ErrInvalid, E.Cause(err, "open the archive")))
	}
	contents, err := automation.ReadArchive(reader)
	if err != nil {
		if !errors.Is(err, device.ErrInvalid) {
			err = E.Cause1(device.ErrInvalid, err)
		}
		return apiError(err)
	}
	host := s.options.Swiplets
	_, missing := host.Swiplet(contents.Manifest.ID)
	installed, err := host.Install(bytes.NewReader(archive.Bytes()), int64(archive.Len()))
	if err != nil {
		return apiError(err)
	}
	s.options.Logger.Info("deployed ", installed.ID, " ", installed.Manifest.Version)
	return stream.SendAndClose(&proto.DeploySwipletResponse{Swiplet: s.swipletOf(installed), Replaced: missing == nil})
}

func (s *swipletService) RemoveSwiplet(_ context.Context, request *proto.RemoveSwipletRequest) (*emptypb.Empty, error) {
	if err := s.options.Swiplets.Remove(request.SwipletId); err != nil {
		return nil, apiError(err)
	}
	return &emptypb.Empty{}, nil
}

func dataPath(name string) (string, error) {
	if strings.HasPrefix(name, "/") && name != "/" {
		return "", E.Extend(device.ErrInvalid, "the path ", strconv.Quote(name), " is absolute; give one within the swiplet's data directory, such as settings.json")
	}
	cleaned := path.Clean(strings.TrimPrefix(name, "/"))
	if cleaned == "." {
		return "", nil
	}
	if !fs.ValidPath(cleaned) {
		return "", E.Extend(device.ErrInvalid, "the path ", strconv.Quote(name), " leaves the data directory")
	}
	return cleaned, nil
}

func fileInfoOf(directory string, file automation.FileInfo) *proto.FileInfo {
	return &proto.FileInfo{Path: path.Join(directory, file.Name), Directory: file.Directory, Size: file.Size, Modified: timestamppb.New(file.Modified)}
}

func (s *swipletService) statFile(id, name string) (*proto.FileInfo, error) {
	if name == "" {
		return &proto.FileInfo{Directory: true}, nil
	}
	parent := path.Dir(name)
	if parent == "." {
		parent = ""
	}
	files, err := s.options.Swiplets.ListFiles(id, automation.DataFolder, parent)
	if err != nil {
		return nil, err
	}
	index := slices.IndexFunc(files, func(file automation.FileInfo) bool { return file.Name == path.Base(name) })
	if index < 0 {
		return nil, automation.FileNotFound("the data directory of "+id, name)
	}
	return fileInfoOf(parent, files[index]), nil
}

func (s *swipletService) walkFiles(id, directory string, visit func(*proto.FileInfo) error) error {
	files, err := s.options.Swiplets.ListFiles(id, automation.DataFolder, directory)
	if err != nil {
		return err
	}
	for _, file := range files {
		info := fileInfoOf(directory, file)
		if err = visit(info); err != nil {
			return err
		}
		if file.Directory {
			if err = s.walkFiles(id, info.Path, visit); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *swipletService) ListDataFiles(_ context.Context, request *proto.ListDataFilesRequest) (*proto.ListDataFilesResponse, error) {
	directory, err := dataPath(request.Path)
	if err != nil {
		return nil, apiError(err)
	}
	var out []*proto.FileInfo
	if request.Recursive {
		err = s.walkFiles(request.SwipletId, directory, func(info *proto.FileInfo) error {
			out = append(out, info)
			return nil
		})
	} else {
		var files []automation.FileInfo
		files, err = s.options.Swiplets.ListFiles(request.SwipletId, automation.DataFolder, directory)
		out = common.Map(files, func(file automation.FileInfo) *proto.FileInfo { return fileInfoOf(directory, file) })
	}
	if err != nil {
		return nil, apiError(err)
	}
	return &proto.ListDataFilesResponse{Files: out}, nil
}

func sendFile(info *proto.FileInfo, data []byte, send func(*proto.FileInfo, *proto.FileChunk) error) error {
	if err := send(info, nil); err != nil {
		return err
	}
	for offset := 0; offset < len(data); offset += chunkSize {
		chunk := &proto.FileChunk{Path: info.Path, Offset: int64(offset), Data: data[offset:min(offset+chunkSize, len(data))]}
		if err := send(nil, chunk); err != nil {
			return err
		}
	}
	return nil
}

func (s *swipletService) ReadDataFile(request *proto.ReadDataFileRequest, stream grpc.ServerStreamingServer[proto.ReadDataFileResponse]) error {
	id := request.SwipletId
	name, err := dataPath(request.Path)
	if err != nil {
		return apiError(err)
	}
	send := func(info *proto.FileInfo, chunk *proto.FileChunk) error {
		if info != nil {
			return stream.Send(&proto.ReadDataFileResponse{Part: &proto.ReadDataFileResponse_File{File: info}})
		}
		return stream.Send(&proto.ReadDataFileResponse{Part: &proto.ReadDataFileResponse_Chunk{Chunk: chunk}})
	}
	info, err := s.statFile(id, name)
	if err != nil {
		return apiError(err)
	}
	if !info.Directory {
		data, err := s.options.Swiplets.ReadFile(id, automation.DataFolder, name)
		if err != nil {
			return apiError(err)
		}
		return sendFile(info, data, send)
	}
	return apiError(s.walkFiles(id, name, func(info *proto.FileInfo) error {
		if info.Directory {
			return send(info, nil)
		}
		data, err := s.options.Swiplets.ReadFile(id, automation.DataFolder, info.Path)
		if err != nil {
			return err
		}
		return sendFile(info, data, send)
	}))
}

func (s *swipletService) WriteDataFile(stream grpc.ClientStreamingServer[proto.WriteDataFileRequest, proto.WriteDataFileResponse]) error {
	var header *proto.WriteDataFileHeader
	var data bytes.Buffer
	for {
		request, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		switch part := request.Part.(type) {
		case *proto.WriteDataFileRequest_Header:
			if header != nil {
				return apiError(E.Extend(device.ErrInvalid, "the header was sent twice"))
			}
			header = part.Header
		case *proto.WriteDataFileRequest_Data:
			if header == nil {
				return apiError(E.Extend(device.ErrInvalid, "the data came before the header"))
			}
			if data.Len()+len(part.Data) > largestUpload {
				return status.Error(codes.ResourceExhausted, F.ToString("the file is larger than ", largestUpload>>20, " MiB"))
			}
			data.Write(part.Data)
		}
	}
	if header == nil {
		return apiError(E.Extend(device.ErrInvalid, "the header is missing"))
	}
	name, err := dataPath(header.Path)
	if err == nil && name == "" {
		err = E.Extend(device.ErrInvalid, "the path names the data directory")
	}
	if err == nil {
		err = s.options.Swiplets.WriteFile(header.SwipletId, automation.DataFolder, name, data.Bytes())
	}
	if err != nil {
		return apiError(err)
	}
	info, err := s.statFile(header.SwipletId, name)
	if err != nil {
		return apiError(err)
	}
	return stream.SendAndClose(&proto.WriteDataFileResponse{File: info})
}

func (s *swipletService) RemoveDataFile(_ context.Context, request *proto.RemoveDataFileRequest) (*emptypb.Empty, error) {
	id := request.SwipletId
	name, err := dataPath(request.Path)
	if err == nil && name == "" {
		err = E.Extend(device.ErrInvalid, "the path names the data directory")
	}
	var info *proto.FileInfo
	if err == nil {
		info, err = s.statFile(id, name)
	}
	if err == nil && info.Directory && !request.Recursive {
		var files []automation.FileInfo
		if files, err = s.options.Swiplets.ListFiles(id, automation.DataFolder, name); err == nil && len(files) > 0 {
			err = status.Error(codes.FailedPrecondition, name+" is a directory that holds files; remove it recursively")
		}
	}
	if err == nil {
		err = s.options.Swiplets.RemoveFile(id, automation.DataFolder, name)
	}
	if err != nil {
		return nil, apiError(err)
	}
	return &emptypb.Empty{}, nil
}
