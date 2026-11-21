//go:build android && cgo

package core

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"sync"

	E "github.com/sagernet/sing/common/exceptions"
	androidpb "github.com/swipt-automation/swipt/internal/android/proto"
	"github.com/swipt-automation/swipt/internal/api"
	"github.com/swipt-automation/swipt/internal/automation"
	"github.com/swipt-automation/swipt/internal/fault"
	"github.com/swipt-automation/swipt/internal/jsc"
	"github.com/swipt-automation/swipt/internal/log"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"
)

type interfaceService struct {
	androidpb.UnimplementedInterfaceServiceServer
	*Core
	access   sync.Mutex
	sessions map[string]*interfaceSession
}

type interfaceSession struct {
	swiplet string
	web     *automation.WebSession
}

func (s *interfaceService) OpenInterface(request *androidpb.OpenInterfaceRequest, stream grpc.ServerStreamingServer[androidpb.InterfaceEvent]) error {
	web, err := s.host.OpenWeb(request.SwipletId)
	if err != nil {
		return api.StatusError(err)
	}
	session := &interfaceSession{swiplet: request.SwipletId, web: web}
	id := rand.Text()
	s.access.Lock()
	s.sessions[id] = session
	s.access.Unlock()
	defer func() {
		s.access.Lock()
		delete(s.sessions, id)
		s.access.Unlock()
		web.Close()
		s.host.CloseDatabases(session, "")
	}()
	ctx, cancel := context.WithCancelCause(stream.Context())
	defer cancel(nil)
	events := make(chan *androidpb.InterfaceEvent, valueBuffer)
	send := func(event *androidpb.InterfaceEvent) {
		select {
		case events <- event:
		case <-ctx.Done():
		}
	}
	go s.host.WatchDatabases(ctx, request.SwipletId, func(change automation.DatabaseChange) {
		send(&androidpb.InterfaceEvent{Event: &androidpb.InterfaceEvent_DatabaseChange{DatabaseChange: &androidpb.DatabaseChange{
			Database: change.Database, Path: change.Path, Table: change.Table, RowId: change.RowID,
		}}})
	})
	go func() {
		_ = s.host.WatchServices(ctx, func(swiplet string, state automation.ServiceState) {
			if swiplet == request.SwipletId {
				send(&androidpb.InterfaceEvent{Event: &androidpb.InterfaceEvent_Service{Service: serviceStateOf(state)}})
			}
		})
	}()
	if err = stream.Send(&androidpb.InterfaceEvent{Event: &androidpb.InterfaceEvent_Session{Session: id}}); err != nil {
		return err
	}
	for {
		select {
		case event := <-events:
			if err = stream.Send(event); err != nil {
				return err
			}
		case <-ctx.Done():
			return nil
		}
	}
}

func (s *interfaceService) session(id string) (*interfaceSession, error) {
	s.access.Lock()
	defer s.access.Unlock()
	session := s.sessions[id]
	if session == nil {
		return nil, api.StatusError(E.Extend(fault.ErrNotFound, "the interface session has ended"))
	}
	return session, nil
}

func (s *interfaceService) QueryDatabase(ctx context.Context, request *androidpb.QueryDatabaseRequest) (*androidpb.QueryDatabaseResponse, error) {
	session, err := s.session(request.Session)
	if err != nil {
		return nil, err
	}
	result, err := s.host.SQLite(ctx, session, session.swiplet, []byte(request.Request))
	if err != nil {
		return nil, api.StatusError(err)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return nil, api.StatusError(err)
	}
	return &androidpb.QueryDatabaseResponse{Result: string(encoded)}, nil
}

func (s *interfaceService) StartWeb(ctx context.Context, request *androidpb.StartWebRequest) (*androidpb.StartWebResponse, error) {
	session, err := s.session(request.Session)
	if err != nil {
		return nil, err
	}
	done := make(chan *androidpb.StartWebResponse, 1)
	session.web.Start(request.Id, request.Name, request.Arguments, func(value any, err error) {
		done <- webResult(value, err)
	})
	select {
	case result := <-done:
		return result, nil
	case <-ctx.Done():
		session.web.Cancel(request.Id)
		return <-done, nil
	}
}

func webResult(value any, err error) *androidpb.StartWebResponse {
	if err != nil {
		return &androidpb.StartWebResponse{Result: &androidpb.StartWebResponse_Failure{Failure: automation.FailureOf(err)}}
	}
	if data, isData := value.(jsc.Bytes); isData {
		return &androidpb.StartWebResponse{Result: &androidpb.StartWebResponse_Data{Data: data}}
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return &androidpb.StartWebResponse{Result: &androidpb.StartWebResponse_Failure{Failure: automation.FailureOf(err)}}
	}
	return &androidpb.StartWebResponse{Result: &androidpb.StartWebResponse_Json{Json: string(encoded)}}
}

func (s *interfaceService) CancelWeb(_ context.Context, request *androidpb.CancelWebRequest) (*emptypb.Empty, error) {
	session, err := s.session(request.Session)
	if err != nil {
		return nil, err
	}
	session.web.Cancel(request.Id)
	return &emptypb.Empty{}, nil
}

func (s *interfaceService) PutWeb(_ context.Context, request *androidpb.PutWebRequest) (*androidpb.PutWebResponse, error) {
	session, err := s.session(request.Session)
	if err != nil {
		return nil, err
	}
	return &androidpb.PutWebResponse{Id: session.web.Put(request.Data)}, nil
}

func (s *interfaceService) ReleaseWeb(_ context.Context, request *androidpb.ReleaseWebRequest) (*emptypb.Empty, error) {
	session, err := s.session(request.Session)
	if err != nil {
		return nil, err
	}
	session.web.Release(request.Id, request.Reason)
	return &emptypb.Empty{}, nil
}

func (s *interfaceService) OpenWebFile(_ context.Context, request *androidpb.OpenWebFileRequest) (*androidpb.OpenWebFileResponse, error) {
	session, err := s.session(request.Session)
	if err != nil {
		return nil, err
	}
	return &androidpb.OpenWebFileResponse{Id: session.web.OpenFile(request.Root, request.Path)}, nil
}

func (s *interfaceService) OpenWebCompression(_ context.Context, request *androidpb.OpenWebCompressionRequest) (*androidpb.OpenWebCompressionResponse, error) {
	session, err := s.session(request.Session)
	if err != nil {
		return nil, err
	}
	ids, err := session.web.Compression(request.Format, request.Mode)
	if err != nil {
		return nil, api.StatusError(err)
	}
	return &androidpb.OpenWebCompressionResponse{Ids: ids}, nil
}

func (s *interfaceService) WriteLog(_ context.Context, request *androidpb.WriteLogRequest) (*emptypb.Empty, error) {
	if _, err := s.host.Swiplet(request.SwipletId); err != nil {
		return nil, api.StatusError(err)
	}
	level := log.LevelInfo
	switch request.Level {
	case androidpb.LogLevel_LOG_LEVEL_ERROR:
		level = log.LevelError
	case androidpb.LogLevel_LOG_LEVEL_WARN:
		level = log.LevelWarn
	case androidpb.LogLevel_LOG_LEVEL_DEBUG:
		level = log.LevelDebug
	}
	s.host.Log(request.SwipletId, level, request.Message)
	return &emptypb.Empty{}, nil
}
