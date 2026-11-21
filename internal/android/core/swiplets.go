//go:build android && cgo

package core

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/sagernet/sing/common"
	E "github.com/sagernet/sing/common/exceptions"
	F "github.com/sagernet/sing/common/format"
	androidpb "github.com/swipt-automation/swipt/internal/android/proto"
	"github.com/swipt-automation/swipt/internal/api"
	"github.com/swipt-automation/swipt/internal/automation"
	"github.com/swipt-automation/swipt/internal/fault"
	"github.com/swipt-automation/swipt/internal/repository"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	chunkSize     = 256 << 10
	largestUpload = 128 << 20
	valueBuffer   = 1024
)

type swipletService struct {
	androidpb.UnimplementedSwipletServiceServer
	*Core
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

func scriptError(failure *automation.Failure) *androidpb.ScriptError {
	return &androidpb.ScriptError{
		Name: failure.Name, Message: failure.Message,
		Stack: common.Map(failure.Stack, func(frame automation.Frame) *androidpb.StackFrame {
			return &androidpb.StackFrame{Function: frame.Function, File: frame.File, Line: uint32(max(frame.Line, 0)), Column: uint32(max(frame.Column, 0))}
		}),
	}
}

func serviceStateOf(state automation.ServiceState) *androidpb.ServiceState {
	out := &androidpb.ServiceState{Enabled: state.Enabled}
	if state.Failure != nil {
		out.Error = scriptError(state.Failure)
	}
	switch state.Status {
	case automation.ServiceStopped:
		out.Status = androidpb.ServiceStatus_SERVICE_STATUS_STOPPED
	case automation.ServiceStarting:
		out.Status = androidpb.ServiceStatus_SERVICE_STATUS_STARTING
	case automation.ServiceRunning:
		out.Status = androidpb.ServiceStatus_SERVICE_STATUS_RUNNING
	case automation.ServiceFailed:
		out.Status = androidpb.ServiceStatus_SERVICE_STATUS_FAILED
	}
	if !state.Since.IsZero() {
		out.Since = timestamppb.New(state.Since)
	}
	return out
}

func (s *swipletService) manifestOf(manifest automation.Manifest, hasImage bool) *androidpb.Swiplet {
	os := s.host.OS()
	return &androidpb.Swiplet{
		Id: manifest.ID, Title: manifest.Name, LocalizedTitles: manifest.LocalizedNames,
		Version: manifest.Version, VersionCode: manifest.VersionCode, HasService: manifest.Service,
		Icon: &androidpb.SwipletIcon{
			Color: manifest.Icon.Color, Symbol: manifest.Symbol(os), SymbolColor: manifest.Icon.SymbolColor, HasImage: hasImage,
		},
		Actions: common.Map(manifest.Actions, func(action automation.ActionInfo) *androidpb.SwipletAction {
			return &androidpb.SwipletAction{
				Name: action.Name, Title: action.Title, Description: action.Description, Stream: action.Stream,
				InputSchema: schemaOf(action.Input), OutputSchema: schemaOf(action.Output),
			}
		}),
	}
}

func (s *swipletService) swipletOf(swiplet automation.Swiplet) *androidpb.Swiplet {
	_, iconErr := os.Stat(filepath.Join(swiplet.Directory, automation.IconFile))
	out := s.manifestOf(swiplet.Manifest, iconErr == nil)
	if info, err := os.Stat(filepath.Join(swiplet.Directory, automation.ManifestFile)); err == nil {
		out.Installed = timestamppb.New(info.ModTime())
	}
	switch err := s.host.Compatible(swiplet.Manifest); {
	case err == nil:
		interfacePath := filepath.Join(swiplet.Directory, filepath.FromSlash(swiplet.Platform.Sources), automation.InterfaceFile)
		_, interfaceErr := os.Stat(interfacePath)
		out.HasInterface = interfaceErr == nil
	case errors.Is(err, automation.ErrSwipletOutdated):
		out.NeedsUpdate = true
	}
	if out.HasService {
		if state, err := s.host.Service(swiplet.ID); err == nil {
			out.Service = serviceStateOf(state)
		}
	}
	return out
}

func (s *swipletService) listSwiplets() (*androidpb.ListSwipletsResponse, error) {
	swiplets, err := s.host.Swiplets()
	if err != nil {
		return nil, api.StatusError(err)
	}
	return &androidpb.ListSwipletsResponse{Swiplets: common.Map(swiplets, s.swipletOf)}, nil
}

func (s *swipletService) WatchSwiplets(_ *emptypb.Empty, stream grpc.ServerStreamingServer[androidpb.ListSwipletsResponse]) error {
	for {
		changed := s.host.SwipletsChanged()
		listed, err := s.listSwiplets()
		if err != nil {
			return err
		}
		if err = stream.Send(listed); err != nil {
			return err
		}
		select {
		case <-changed:
		case <-stream.Context().Done():
			return nil
		}
	}
}

func inputOf(value *structpb.Value) (json.RawMessage, error) {
	if value == nil {
		return nil, nil
	}
	data, err := protojson.Marshal(value)
	if err != nil {
		return nil, E.Cause1(fault.ErrInvalid, err)
	}
	return data, nil
}

func actionResult(output json.RawMessage, err error) (*androidpb.ActionResult, error) {
	if failure, isFailure := automation.ScriptFailure(err); isFailure {
		return &androidpb.ActionResult{Outcome: &androidpb.ActionResult_Error{Error: scriptError(failure)}}, nil
	}
	if err != nil {
		return nil, api.StatusError(err)
	}
	value := &structpb.Value{}
	if err = protojson.Unmarshal(output, value); err != nil {
		return nil, api.StatusError(E.Cause(err, "read the output"))
	}
	return &androidpb.ActionResult{Outcome: &androidpb.ActionResult_Output{Output: value}}, nil
}

type execution struct {
	output json.RawMessage
	err    error
}

func (s *swipletService) CallAction(request *androidpb.CallActionRequest, stream grpc.ServerStreamingServer[androidpb.CallActionResponse]) error {
	ctx, cancel := context.WithCancelCause(stream.Context())
	defer cancel(nil)
	input, err := inputOf(request.Input)
	if err != nil {
		return api.StatusError(err)
	}
	parts := make(chan *androidpb.CallActionResponse, valueBuffer)
	done := make(chan execution, 1)
	go func() {
		output, err := s.host.Call(ctx, automation.CallRequest{
			Swiplet: request.SwipletId, Action: request.Action, Input: input, Caller: automation.CallerApp,
			Emit: func(data json.RawMessage) {
				value := &structpb.Value{}
				if err := protojson.Unmarshal(data, value); err != nil {
					cancel(E.Cause(err, "read the value"))
					return
				}
				select {
				case parts <- &androidpb.CallActionResponse{Part: &androidpb.CallActionResponse_Value{Value: value}}:
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
	if cause := context.Cause(ctx); cause != nil && stream.Context().Err() == nil {
		return api.StatusError(cause)
	}
	if result.err == nil && len(result.output) == 0 {
		return stream.Send(&androidpb.CallActionResponse{Part: &androidpb.CallActionResponse_Result{Result: &androidpb.ActionResult{}}})
	}
	out, err := actionResult(result.output, result.err)
	if err != nil {
		return err
	}
	return stream.Send(&androidpb.CallActionResponse{Part: &androidpb.CallActionResponse_Result{Result: out}})
}

func (s *swipletService) SetServiceEnabled(ctx context.Context, request *androidpb.SetServiceEnabledRequest) (*androidpb.SetServiceEnabledResponse, error) {
	requested := time.Now()
	if err := s.host.SetServiceEnabled(request.SwipletId, request.Enabled); err != nil {
		return nil, api.StatusError(err)
	}
	state, err := s.host.AwaitService(ctx, request.SwipletId, request.Enabled, requested)
	if err != nil {
		return nil, api.StatusError(err)
	}
	return &androidpb.SetServiceEnabledResponse{Service: serviceStateOf(state)}, nil
}

func (s *swipletService) WatchServices(request *androidpb.WatchServicesRequest, stream grpc.ServerStreamingServer[androidpb.WatchServicesResponse]) error {
	if id := request.SwipletId; id != "" {
		if _, err := s.host.Service(id); err != nil {
			return api.StatusError(err)
		}
	}
	ctx, cancel := context.WithCancelCause(stream.Context())
	defer cancel(nil)
	err := s.host.WatchServices(ctx, func(id string, state automation.ServiceState) {
		if request.SwipletId != "" && id != request.SwipletId {
			return
		}
		if err := stream.Send(&androidpb.WatchServicesResponse{SwipletId: id, Service: serviceStateOf(state)}); err != nil {
			cancel(err)
		}
	})
	if cause := context.Cause(ctx); cause != nil && !errors.Is(cause, context.Canceled) {
		return cause
	}
	return api.StatusError(err)
}

func receiveArchive(recv func() (*androidpb.DeploySwipletRequest, error)) (*bytes.Reader, automation.Archive, error) {
	var archive bytes.Buffer
	for {
		request, err := recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, automation.Archive{}, err
		}
		if archive.Len()+len(request.Chunk) > largestUpload {
			return nil, automation.Archive{}, status.Error(codes.ResourceExhausted, F.ToString("the archive is larger than ", largestUpload>>20, " MiB"))
		}
		archive.Write(request.Chunk)
	}
	reader := bytes.NewReader(archive.Bytes())
	zipped, err := zip.NewReader(reader, reader.Size())
	if err != nil {
		return nil, automation.Archive{}, api.StatusError(E.Cause1(fault.ErrInvalid, E.Cause(err, "open the archive")))
	}
	contents, err := automation.ReadArchive(zipped)
	if err != nil {
		if !errors.Is(err, fault.ErrInvalid) {
			err = E.Cause1(fault.ErrInvalid, err)
		}
		return nil, automation.Archive{}, api.StatusError(err)
	}
	return reader, contents, nil
}

func incompatibilityOf(err error) (androidpb.Incompatibility, string) {
	switch {
	case err == nil:
		return androidpb.Incompatibility_INCOMPATIBILITY_UNSPECIFIED, ""
	case errors.Is(err, automation.ErrOutdated):
		return androidpb.Incompatibility_INCOMPATIBILITY_SWIPT_OUTDATED, ""
	case errors.Is(err, automation.ErrSwipletOutdated):
		return androidpb.Incompatibility_INCOMPATIBILITY_SWIPLET_OUTDATED, ""
	}
	if outdated, isOutdated := errors.AsType[*automation.OSOutdatedError](err); isOutdated {
		return androidpb.Incompatibility_INCOMPATIBILITY_OS_OUTDATED, outdated.Minimum
	}
	return androidpb.Incompatibility_INCOMPATIBILITY_OS_UNSUPPORTED, ""
}

func (s *swipletService) InspectSwiplet(stream grpc.ClientStreamingServer[androidpb.DeploySwipletRequest, androidpb.InspectSwipletResponse]) error {
	_, contents, err := receiveArchive(stream.Recv)
	if err != nil {
		return err
	}
	manifest := contents.Manifest
	var icon []byte
	for _, file := range contents.Files {
		if file.Name != automation.IconFile {
			continue
		}
		reader, err := file.Open()
		if err != nil {
			return api.StatusError(err)
		}
		icon, err = io.ReadAll(reader)
		_ = reader.Close()
		if err != nil {
			return api.StatusError(err)
		}
	}
	response := &androidpb.InspectSwipletResponse{
		Swiplet: s.manifestOf(manifest, icon != nil),
		Icon:    icon,
	}
	if err := s.host.Compatible(manifest); err != nil {
		response.Incompatibility, response.MinimumOsVersion = incompatibilityOf(err)
		response.IncompatibilityMessage = err.Error()
	}
	return stream.SendAndClose(response)
}

func (s *swipletService) DeploySwiplet(stream grpc.ClientStreamingServer[androidpb.DeploySwipletRequest, androidpb.DeploySwipletResponse]) error {
	reader, contents, err := receiveArchive(stream.Recv)
	if err != nil {
		return err
	}
	_, missing := s.host.Swiplet(contents.Manifest.ID)
	installed, err := s.host.Install(reader, reader.Size())
	if err != nil {
		return api.StatusError(err)
	}
	s.logger.Info("deployed ", installed.ID, " ", installed.Manifest.Version)
	return stream.SendAndClose(&androidpb.DeploySwipletResponse{Swiplet: s.swipletOf(installed), Replaced: missing == nil})
}

func (s *swipletService) RemoveSwiplet(_ context.Context, request *androidpb.RemoveSwipletRequest) (*emptypb.Empty, error) {
	if err := s.host.Remove(request.SwipletId); err != nil {
		return nil, api.StatusError(err)
	}
	return &emptypb.Empty{}, nil
}

func (s *swipletService) GetSwipletIcon(_ context.Context, request *androidpb.GetSwipletIconRequest) (*androidpb.GetSwipletIconResponse, error) {
	swiplet, err := s.host.Swiplet(request.SwipletId)
	if err != nil {
		return nil, api.StatusError(err)
	}
	image, err := os.ReadFile(filepath.Join(swiplet.Directory, automation.IconFile))
	if err != nil {
		return nil, api.StatusError(err)
	}
	return &androidpb.GetSwipletIconResponse{Image: image}, nil
}

func (s *swipletService) GetSwipletInterface(_ context.Context, request *androidpb.GetSwipletInterfaceRequest) (*androidpb.GetSwipletInterfaceResponse, error) {
	swiplet, err := s.host.Swiplet(request.SwipletId)
	if err == nil {
		err = s.host.Compatible(swiplet.Manifest)
	}
	if err != nil {
		return nil, api.StatusError(err)
	}
	file := path.Join(swiplet.Platform.Sources, automation.InterfaceFile)
	source, err := os.ReadFile(filepath.Join(swiplet.Directory, filepath.FromSlash(file)))
	if err != nil {
		return nil, api.StatusError(err)
	}
	return &androidpb.GetSwipletInterfaceResponse{
		File: file, Source: string(source),
		DataDirectory:   filepath.Join(swiplet.Directory, automation.DataFolder),
		AssetsDirectory: filepath.Join(swiplet.Directory, automation.AssetsFolder),
		CacheDirectory:  s.host.CacheDirectory(swiplet.ID),
	}, nil
}

func (s *swipletService) ReadSwipletFile(request *androidpb.ReadSwipletFileRequest, stream grpc.ServerStreamingServer[androidpb.ReadSwipletFileResponse]) error {
	swiplet, err := s.host.Swiplet(request.SwipletId)
	if err != nil {
		return api.StatusError(err)
	}
	var directory string
	switch request.Root {
	case automation.DataFolder:
		directory = filepath.Join(swiplet.Directory, automation.DataFolder)
	case automation.AssetsFolder:
		directory = filepath.Join(swiplet.Directory, automation.AssetsFolder)
	case "cache":
		directory = s.host.CacheDirectory(swiplet.ID)
	default:
		return api.StatusError(E.Extend(fault.ErrInvalid, "unknown file root ", strconv.Quote(request.Root)))
	}
	cleaned := path.Clean(strings.TrimPrefix(request.Path, "/"))
	local := filepath.FromSlash(cleaned)
	if cleaned == "." || cleaned == "" || !fs.ValidPath(cleaned) || !filepath.IsLocal(local) {
		return api.StatusError(E.Extend(fault.ErrInvalid, "invalid file path ", strconv.Quote(request.Path)))
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return api.StatusError(err)
	}
	defer root.Close()
	file, err := root.Open(local)
	if err != nil {
		return api.StatusError(err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return api.StatusError(err)
	}
	if info.IsDir() {
		return api.StatusError(E.Extend(fault.ErrInvalid, strconv.Quote(cleaned), " is a directory"))
	}
	buffer := make([]byte, chunkSize)
	for {
		count, readErr := file.Read(buffer)
		if count > 0 {
			if err = stream.Send(&androidpb.ReadSwipletFileResponse{Chunk: buffer[:count]}); err != nil {
				return err
			}
		}
		if errors.Is(readErr, io.EOF) {
			return nil
		}
		if readErr != nil {
			return api.StatusError(readErr)
		}
	}
}

type chunkWriter func([]byte) error

func (w chunkWriter) Write(data []byte) (int, error) {
	for offset := 0; offset < len(data); offset += chunkSize {
		if err := w(data[offset:min(offset+chunkSize, len(data))]); err != nil {
			return offset, err
		}
	}
	return len(data), nil
}

func (s *swipletService) ExportSwiplet(request *androidpb.ExportSwipletRequest, stream grpc.ServerStreamingServer[androidpb.ExportSwipletResponse]) error {
	return api.StatusError(s.host.Export(request.SwipletId, chunkWriter(func(chunk []byte) error {
		return stream.Send(&androidpb.ExportSwipletResponse{Chunk: chunk})
	})))
}

func (s *swipletService) GetSwipletDataSize(_ context.Context, request *androidpb.GetSwipletDataSizeRequest) (*androidpb.GetSwipletDataSizeResponse, error) {
	swiplet, err := s.host.Swiplet(request.SwipletId)
	if err != nil {
		return nil, api.StatusError(err)
	}
	return &androidpb.GetSwipletDataSizeResponse{Bytes: allocatedSize(filepath.Join(swiplet.Directory, automation.DataFolder))}, nil
}

func (s *swipletService) ClearSwipletData(_ context.Context, request *androidpb.ClearSwipletDataRequest) (*emptypb.Empty, error) {
	if err := s.host.ClearData(request.SwipletId); err != nil {
		return nil, api.StatusError(err)
	}
	return &emptypb.Empty{}, nil
}

func repositoryIconOf(icon repository.Icon) *androidpb.SwipletIcon {
	return &androidpb.SwipletIcon{Color: icon.Color, Symbol: icon.Symbol, SymbolColor: icon.SymbolColor}
}

func (s *swipletService) FetchRepository(ctx context.Context, request *androidpb.FetchRepositoryRequest) (*androidpb.FetchRepositoryResponse, error) {
	fetched, err := repository.Fetch(ctx, request.Url, request.EntityTag, s.host.OS())
	if err != nil {
		return nil, api.StatusError(err)
	}
	response := &androidpb.FetchRepositoryResponse{NotModified: fetched.NotModified, EntityTag: fetched.EntityTag}
	if fetched.NotModified {
		return response, nil
	}
	response.Repository = &androidpb.Repository{Name: fetched.Name, LocalizedNames: fetched.LocalizedNames, Image: fetched.Image}
	if fetched.Icon != nil {
		response.Repository.Icon = repositoryIconOf(*fetched.Icon)
	}
	for _, swiplet := range fetched.Swiplets {
		response.Repository.Swiplets = append(response.Repository.Swiplets, &androidpb.RepositorySwiplet{
			Id: swiplet.ID, Title: swiplet.Name, LocalizedTitles: swiplet.LocalizedNames, Description: swiplet.Description,
			Version: swiplet.Version, VersionCode: swiplet.VersionCode, MinimumApiLevel: swiplet.MinimumAPILevel,
			MinimumOsVersion: swiplet.MinimumOSVersion, Icon: repositoryIconOf(swiplet.Icon),
			Url: swiplet.URL, Sha256: swiplet.SHA256,
		})
	}
	return response, nil
}

func (s *swipletService) DownloadSwiplet(request *androidpb.DownloadSwipletRequest, stream grpc.ServerStreamingServer[androidpb.DownloadSwipletResponse]) error {
	return api.StatusError(repository.Transfer(stream.Context(), request.Url, request.Sha256, func(total int64) (io.Writer, error) {
		return chunkWriter(func(chunk []byte) error {
			return stream.Send(&androidpb.DownloadSwipletResponse{Total: total, Chunk: chunk})
		}), nil
	}))
}

func (s *swipletService) CheckCompatibility(_ context.Context, request *androidpb.CheckCompatibilityRequest) (*androidpb.CheckCompatibilityResponse, error) {
	os := s.host.OS()
	response := &androidpb.CheckCompatibilityResponse{}
	for _, requirement := range request.Requirements {
		incompatibility, _ := incompatibilityOf(s.host.Compatible(automation.Manifest{
			MinimumAPILevel: requirement.MinimumApiLevel,
			Platforms:       map[automation.OS]automation.Platform{os: automation.PlatformRequiring(requirement.MinimumOsVersion)},
		}))
		response.Incompatibilities = append(response.Incompatibilities, incompatibility)
	}
	return response, nil
}
