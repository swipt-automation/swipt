//go:build android && cgo

package core

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	mathrand "math/rand/v2"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/sagernet/sing/common"
	E "github.com/sagernet/sing/common/exceptions"
	"github.com/swipt-automation/swipt/internal/android"
	"github.com/swipt-automation/swipt/internal/android/proto"
	"github.com/swipt-automation/swipt/internal/api"
	"github.com/swipt-automation/swipt/internal/atomicfile"
	"github.com/swipt-automation/swipt/internal/cloudflare"
	"github.com/swipt-automation/swipt/internal/log"
	"github.com/swipt-automation/swipt/internal/tailscale"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

const (
	settingsFile      = "settings.json"
	setupRequestFile  = "/data/local/tmp/swipt-setup.json"
	setupResultFile   = "/data/local/tmp/swipt-setup.done"
	shellUID          = 2000
	shellGID          = 2000
	zoneLookupTimeout = 30 * time.Second
	firstRandomPort   = 10000
	lastRandomPort    = 49151
)

type setupRequest struct {
	ID             string `json:"id"`
	UnlockType     string `json:"unlockType,omitempty"`
	UnlockPasscode string `json:"unlockPasscode,omitempty"`
	UnlockIfUnset  bool   `json:"unlockIfUnset,omitempty"`
	LocalNetwork   *bool  `json:"localNetwork,omitempty"`
}

type setupResult struct {
	ID            string `json:"id"`
	Port          int    `json:"port"`
	Authenticates bool   `json:"authenticates"`
	Error         string `json:"error,omitempty"`
}

type settings struct {
	AutoUnlock     bool               `json:"autoUnlock"`
	UnlockType     string             `json:"unlockType,omitempty"`
	UnlockPasscode string             `json:"unlockPasscode,omitempty"`
	LocalNetwork   localNetwork       `json:"localNetwork"`
	Cloudflare     cloudflareSettings `json:"cloudflare"`
	Tailscale      tailscaleSettings  `json:"tailscale"`
}

type localNetwork struct {
	Enabled       bool   `json:"enabled"`
	Port          int    `json:"port"`
	Authenticates bool   `json:"authenticates"`
	Token         string `json:"token"`
}

type cloudflareAccount struct {
	Credential string `json:"credential"`
	Name       string `json:"name"`
	Zone       string `json:"zone"`
}

type cloudflareSettings struct {
	Enabled       bool               `json:"enabled"`
	Mode          cloudflare.Mode    `json:"mode"`
	Path          string             `json:"path"`
	Token         string             `json:"token"`
	Account       *cloudflareAccount `json:"account,omitempty"`
	Hostname      string             `json:"hostname,omitempty"`
	QuickTunnel   string             `json:"quickTunnel,omitempty"`
	QuickHostname string             `json:"quickHostname,omitempty"`
	NamedTunnel   string             `json:"namedTunnel,omitempty"`
}

type tailscaleSettings struct {
	Enabled       bool   `json:"enabled"`
	Authenticates bool   `json:"authenticates"`
	Token         string `json:"token"`
}

func unlockTypeFromProto(kind proto.UnlockType) android.UnlockType {
	switch kind {
	case proto.UnlockType_UNLOCK_TYPE_UNSPECIFIED:
		return android.UnlockDisabled
	case proto.UnlockType_UNLOCK_TYPE_NONE:
		return android.UnlockNone
	case proto.UnlockType_UNLOCK_TYPE_PATTERN:
		return android.UnlockPattern
	case proto.UnlockType_UNLOCK_TYPE_PIN:
		return android.UnlockPIN
	case proto.UnlockType_UNLOCK_TYPE_PASSWORD:
		return android.UnlockPassword
	default:
		return android.UnlockType(kind)
	}
}

func unlockTypeName(kind android.UnlockType) string {
	switch kind {
	case android.UnlockNone:
		return "none"
	case android.UnlockPattern:
		return "pattern"
	case android.UnlockPIN:
		return "pin"
	case android.UnlockPassword:
		return "password"
	default:
		return ""
	}
}

func unlockTypeProto(name string) proto.UnlockType {
	switch name {
	case "none":
		return proto.UnlockType_UNLOCK_TYPE_NONE
	case "pattern":
		return proto.UnlockType_UNLOCK_TYPE_PATTERN
	case "pin":
		return proto.UnlockType_UNLOCK_TYPE_PIN
	case "password":
		return proto.UnlockType_UNLOCK_TYPE_PASSWORD
	default:
		return proto.UnlockType_UNLOCK_TYPE_UNSPECIFIED
	}
}

func effectiveCredential(autoUnlock bool, unlockType android.UnlockType, passcode string) (android.UnlockType, string) {
	if !autoUnlock {
		return android.UnlockDisabled, ""
	}
	return unlockType, passcode
}

func newToken() string {
	token := make([]byte, 16)
	_, _ = rand.Read(token)
	return hex.EncodeToString(token)
}

func loadSettings(directory string) (settings, error) {
	var loaded settings
	data, err := os.ReadFile(filepath.Join(directory, settingsFile))
	switch {
	case err == nil:
		if err = json.Unmarshal(data, &loaded); err != nil {
			return settings{}, E.Cause(err, "read ", settingsFile)
		}
	case !errors.Is(err, fs.ErrNotExist):
		return settings{}, err
	}
	if loaded.LocalNetwork.Port == 0 {
		loaded.LocalNetwork.Port = firstRandomPort + mathrand.IntN(lastRandomPort-firstRandomPort+1)
	}
	if loaded.LocalNetwork.Token == "" {
		loaded.LocalNetwork.Token = newToken()
	}
	if loaded.Cloudflare.Path == "" {
		loaded.Cloudflare.Path = "/"
	}
	if loaded.Cloudflare.Token == "" {
		loaded.Cloudflare.Token = newToken()
	}
	if loaded.Tailscale.Token == "" {
		loaded.Tailscale.Token = newToken()
	}
	return loaded, nil
}

type cloudflareConfiguration struct {
	mode     cloudflare.Mode
	account  string
	hostname string
}

type settingsService struct {
	proto.UnimplementedSettingsServiceServer
	core      *Core
	directory string
	logger    log.ContextLogger
	logs      *log.Factory

	setupAccess     sync.Mutex
	reconcileAccess sync.Mutex
	access          sync.Mutex
	settings        settings
	settingsChanged chan struct{}
	status          *proto.PublicationStatus
	changed         chan struct{}
	tunnel          *cloudflare.Tunnel
	tunnelConfig    *cloudflareConfiguration
	tunnelSequence  uint64
	tailscaleActive *bool

	advertising   sync.Mutex
	advertisement *advertisement
}

func newSettingsService(c *Core, directory string, stored settings, logs *log.Factory) *settingsService {
	return &settingsService{
		core: c, directory: directory, logger: logs.NewLogger("settings"), logs: logs,
		settings: stored, settingsChanged: make(chan struct{}),
		status: &proto.PublicationStatus{LocalNetworkHostname: c.defaultLocalHostname()}, changed: make(chan struct{}),
	}
}

func (s *settingsService) saveLocked() error {
	data, err := json.MarshalIndent(s.settings, "", "  ")
	if err != nil {
		return err
	}
	if err = os.MkdirAll(s.directory, 0o700); err != nil {
		return err
	}
	return atomicfile.Write(filepath.Join(s.directory, settingsFile), data, 0o600)
}

func (s *settingsService) notifySettingsLocked() {
	close(s.settingsChanged)
	s.settingsChanged = make(chan struct{})
}

func (s *settingsService) message() *proto.Settings {
	stored := s.settings
	out := &proto.Settings{
		AutoUnlock:     stored.AutoUnlock,
		UnlockType:     unlockTypeProto(stored.UnlockType),
		UnlockPasscode: stored.UnlockPasscode,
		LocalNetwork: &proto.LocalNetworkSettings{
			Enabled: stored.LocalNetwork.Enabled, Port: uint32(stored.LocalNetwork.Port), Authenticates: stored.LocalNetwork.Authenticates, Token: stored.LocalNetwork.Token,
		},
		Cloudflare: &proto.CloudflareSettings{
			Enabled: stored.Cloudflare.Enabled, Path: stored.Cloudflare.Path, Token: stored.Cloudflare.Token, Hostname: stored.Cloudflare.Hostname,
		},
		Tailscale: &proto.TailscaleSettings{Enabled: stored.Tailscale.Enabled, Authenticates: stored.Tailscale.Authenticates, Token: stored.Tailscale.Token},
	}
	if stored.Cloudflare.Mode == cloudflare.ModeTunnel {
		out.Cloudflare.Mode = proto.CloudflareMode_CLOUDFLARE_MODE_TUNNEL
	}
	if account := stored.Cloudflare.Account; account != nil {
		out.Cloudflare.Account = &proto.CloudflareAccount{Credential: account.Credential, Name: account.Name, Zone: account.Zone}
	}
	return out
}

func (s *settingsService) GetSettings(context.Context, *emptypb.Empty) (*proto.Settings, error) {
	s.access.Lock()
	defer s.access.Unlock()
	return s.message(), nil
}

func (s *settingsService) WatchSettings(_ *emptypb.Empty, stream grpc.ServerStreamingServer[proto.Settings]) error {
	for {
		s.access.Lock()
		current, changed := s.message(), s.settingsChanged
		s.access.Unlock()
		if err := stream.Send(current); err != nil {
			return err
		}
		select {
		case <-changed:
		case <-stream.Context().Done():
			return nil
		}
	}
}

func invalid(message ...any) error {
	return status.Error(codes.InvalidArgument, E.New(message...).Error())
}

func (s *settingsService) UpdateSettings(_ context.Context, request *proto.Settings) (*proto.Settings, error) {
	local, tunnel, tailnet := request.GetLocalNetwork(), request.GetCloudflare(), request.GetTailscale()
	switch {
	case local.GetPort() == 0 || local.GetPort() > 65535:
		return nil, invalid("invalid port ", local.GetPort())
	case strings.TrimSpace(local.GetToken()) == "", strings.TrimSpace(tunnel.GetToken()) == "", strings.TrimSpace(tailnet.GetToken()) == "":
		return nil, invalid("empty token")
	case !strings.HasPrefix(tunnel.GetPath(), "/"):
		return nil, invalid("invalid path ", tunnel.GetPath())
	}
	mode := cloudflare.ModeQuickTunnel
	var account *cloudflareAccount
	if tunnel.GetMode() == proto.CloudflareMode_CLOUDFLARE_MODE_TUNNEL {
		mode = cloudflare.ModeTunnel
	}
	if given := tunnel.GetAccount(); given != nil {
		if _, err := cloudflare.ParseAccount(given.Credential); err != nil {
			return nil, invalid(err)
		}
		account = &cloudflareAccount{Credential: given.Credential, Name: given.Name, Zone: given.Zone}
	}
	configuredType := unlockTypeFromProto(request.UnlockType)
	if request.AutoUnlock && configuredType == android.UnlockDisabled {
		return nil, invalid("automatic unlock requires a screen lock configuration")
	}
	if err := android.ValidateCredential(configuredType, request.UnlockPasscode); err != nil {
		return nil, invalid(err)
	}
	s.core.device.SetCredential(effectiveCredential(request.AutoUnlock, configuredType, request.UnlockPasscode))
	s.access.Lock()
	previous := s.settings.Cloudflare
	if account == nil || previous.Account == nil || account.Credential != previous.Account.Credential {
		previous.NamedTunnel = ""
	}
	s.settings = settings{
		AutoUnlock:     request.AutoUnlock,
		UnlockType:     unlockTypeName(configuredType),
		UnlockPasscode: request.UnlockPasscode,
		LocalNetwork:   localNetwork{Enabled: local.GetEnabled(), Port: int(local.GetPort()), Authenticates: local.GetAuthenticates(), Token: local.GetToken()},
		Cloudflare: cloudflareSettings{
			Enabled: tunnel.GetEnabled(), Mode: mode, Path: tunnel.GetPath(), Token: tunnel.GetToken(), Account: account,
			Hostname: tunnel.GetHostname(), QuickTunnel: previous.QuickTunnel, QuickHostname: previous.QuickHostname, NamedTunnel: previous.NamedTunnel,
		},
		Tailscale: tailscaleSettings{Enabled: tailnet.GetEnabled(), Authenticates: tailnet.GetAuthenticates(), Token: tailnet.GetToken()},
	}
	err := s.saveLocked()
	if err == nil {
		s.notifySettingsLocked()
	}
	response := s.message()
	s.access.Unlock()
	if err != nil {
		return nil, status.Error(codes.Internal, E.Cause(err, "save the settings").Error())
	}
	s.reconcile()
	return response, nil
}

func (s *settingsService) unpublish() {
	s.access.Lock()
	s.settings.LocalNetwork.Enabled = false
	s.settings.Cloudflare.Enabled = false
	s.settings.Tailscale.Enabled = false
	if err := s.saveLocked(); err != nil {
		s.logger.Error(E.Cause(err, "save the settings"))
	}
	s.notifySettingsLocked()
	s.access.Unlock()
	go s.reconcile()
}

func (s *settingsService) updateStatus(update func(status *proto.PublicationStatus)) {
	s.access.Lock()
	defer s.access.Unlock()
	next := &proto.PublicationStatus{
		LocalNetwork: s.status.LocalNetwork, LocalNetworkError: s.status.LocalNetworkError, LocalNetworkHostname: s.status.LocalNetworkHostname,
		CloudflareHostname: s.status.CloudflareHostname, CloudflareError: s.status.CloudflareError,
		Tailscale: s.status.Tailscale, TailscaleError: s.status.TailscaleError,
	}
	update(next)
	s.status = next
	close(s.changed)
	s.changed = make(chan struct{})
}

func (s *settingsService) WatchPublication(_ *emptypb.Empty, stream grpc.ServerStreamingServer[proto.PublicationStatus]) error {
	for {
		s.access.Lock()
		current, changed := s.status, s.changed
		s.access.Unlock()
		if err := stream.Send(current); err != nil {
			return err
		}
		select {
		case <-changed:
		case <-stream.Context().Done():
			return nil
		}
	}
}

func (s *settingsService) start() {
	if err := s.applySetup(true); err != nil {
		s.logger.Error(E.Cause(err, "apply setup"))
	}
}

func (s *settingsService) ApplySetup(context.Context, *emptypb.Empty) (*emptypb.Empty, error) {
	if err := s.applySetup(false); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &emptypb.Empty{}, nil
}

func (s *settingsService) applySetup(reconcileWhenAbsent bool) error {
	s.setupAccess.Lock()
	defer s.setupAccess.Unlock()
	data, err := os.ReadFile(setupRequestFile)
	if errors.Is(err, fs.ErrNotExist) {
		if reconcileWhenAbsent {
			s.reconcile()
		}
		return nil
	}
	_ = os.Remove(setupRequestFile)
	if err != nil {
		if reconcileWhenAbsent {
			s.reconcile()
		}
		return err
	}
	var request setupRequest
	if err = json.Unmarshal(data, &request); err != nil {
		writeSetupResult(setupResult{Error: err.Error()})
		if reconcileWhenAbsent {
			s.reconcile()
		}
		return err
	}
	s.access.Lock()
	next := s.settings
	if request.UnlockType != "" && (!request.UnlockIfUnset || next.UnlockType == "") {
		configuredType := unlockTypeFromProto(unlockTypeProto(request.UnlockType))
		if configuredType == android.UnlockDisabled {
			s.access.Unlock()
			err = E.New("unknown unlock type ", request.UnlockType)
			writeSetupResult(setupResult{ID: request.ID, Error: err.Error()})
			return err
		}
		if err = android.ValidateCredential(configuredType, request.UnlockPasscode); err != nil {
			s.access.Unlock()
			writeSetupResult(setupResult{ID: request.ID, Error: err.Error()})
			return err
		}
		next.AutoUnlock = true
		next.UnlockType = unlockTypeName(configuredType)
		next.UnlockPasscode = request.UnlockPasscode
		s.core.device.SetCredential(effectiveCredential(true, configuredType, request.UnlockPasscode))
	}
	if request.LocalNetwork != nil {
		next.LocalNetwork.Enabled = *request.LocalNetwork
	}
	s.settings = next
	err = s.saveLocked()
	if err == nil {
		s.notifySettingsLocked()
	}
	port, authenticates := next.LocalNetwork.Port, next.LocalNetwork.Authenticates
	s.access.Unlock()
	if err != nil {
		err = E.Cause(err, "save the settings")
		writeSetupResult(setupResult{ID: request.ID, Error: err.Error()})
		return err
	}
	s.reconcile()
	s.access.Lock()
	localErr := s.status.LocalNetworkError
	s.access.Unlock()
	writeSetupResult(setupResult{ID: request.ID, Port: port, Authenticates: authenticates, Error: localErr})
	return nil
}

func writeSetupResult(result setupResult) {
	encoded, err := json.Marshal(result)
	if err != nil {
		return
	}
	temp := setupResultFile + "." + newToken()
	if err = os.WriteFile(temp, encoded, 0o600); err != nil {
		return
	}
	if os.Getuid() == 0 {
		_ = os.Chown(temp, shellUID, shellGID)
	}
	_ = os.Remove(setupResultFile)
	if err = os.Rename(temp, setupResultFile); err != nil {
		_ = os.Remove(temp)
	}
}

func (s *settingsService) reconcile() {
	s.reconcileAccess.Lock()
	defer s.reconcileAccess.Unlock()
	s.access.Lock()
	stored := s.settings
	s.access.Unlock()
	s.reconcileCloudflare(stored.Cloudflare)
	s.reconcileTailscale(stored.Tailscale.Enabled)
	s.access.Lock()
	publication := &api.Publication{
		LocalNetwork: stored.LocalNetwork.Enabled, Port: stored.LocalNetwork.Port,
		Cloudflare: s.tunnel, CloudflarePath: stored.Cloudflare.Path, CloudflareToken: stored.Cloudflare.Token,
	}
	if stored.LocalNetwork.Authenticates {
		publication.LocalNetworkToken = stored.LocalNetwork.Token
	}
	s.access.Unlock()
	if stored.Tailscale.Enabled {
		publication.Tailscale = s.core.tailscale.RunningNode()
		if stored.Tailscale.Authenticates {
			publication.TailscaleToken = stored.Tailscale.Token
		}
	}
	err := s.core.publishAPI(publication)
	publishErr, _ := errors.AsType[*api.PublishError](err)
	var localErr, cloudflareErr, tailscaleErr error
	if publishErr != nil {
		localErr, cloudflareErr, tailscaleErr = publishErr.Local, publishErr.Cloudflare, publishErr.Tailscale
	} else if err != nil {
		localErr = err
	}
	localPublished := localErr == nil && stored.LocalNetwork.Enabled
	s.updateStatus(func(status *proto.PublicationStatus) {
		status.LocalNetwork = localPublished
		status.LocalNetworkError = ""
		if localErr != nil {
			status.LocalNetworkError = localErr.Error()
		}
		if cloudflareErr != nil {
			status.CloudflareError = cloudflareErr.Error()
		}
		if tailscaleErr != nil {
			status.TailscaleError = tailscaleErr.Error()
		}
	})
	s.advertise(stored.LocalNetwork, localPublished)
}

func (s *settingsService) reconcileCloudflare(stored cloudflareSettings) {
	var configuration *cloudflareConfiguration
	if stored.Enabled && (stored.Mode == cloudflare.ModeQuickTunnel || stored.Account != nil && stored.Hostname != "") {
		configuration = &cloudflareConfiguration{mode: stored.Mode}
		if stored.Mode == cloudflare.ModeTunnel {
			configuration.account, configuration.hostname = stored.Account.Credential, stored.Hostname
		}
	}
	s.access.Lock()
	if (configuration == nil) == (s.tunnelConfig == nil) && (configuration == nil || *configuration == *s.tunnelConfig) {
		s.access.Unlock()
		return
	}
	previous := s.tunnel
	s.tunnel, s.tunnelConfig = nil, configuration
	s.tunnelSequence++
	sequence := s.tunnelSequence
	s.access.Unlock()
	if previous != nil {
		if err := previous.Close(); err != nil {
			s.logger.Error(E.Cause(err, "close the Cloudflare tunnel"))
		}
	}
	s.updateStatus(func(status *proto.PublicationStatus) {
		status.CloudflareError = ""
		status.CloudflareHostname = ""
		if configuration != nil && configuration.mode == cloudflare.ModeTunnel {
			status.CloudflareHostname = configuration.hostname
		}
	})
	if configuration == nil {
		return
	}
	options := cloudflare.Options{
		Mode: configuration.mode, Hostname: configuration.hostname, Logger: s.logs.NewLogger("cloudflare"),
		OnState: func(state string, hostname string) { s.cloudflareState(sequence, configuration.mode, state, hostname) },
		OnError: func(err error) { s.cloudflareFailed(sequence, err) },
	}
	options.State = stored.QuickTunnel
	if configuration.mode == cloudflare.ModeTunnel {
		account, err := cloudflare.ParseAccount(configuration.account)
		if err != nil {
			s.cloudflareFailed(sequence, err)
			return
		}
		options.Account, options.State = account, stored.NamedTunnel
	}
	tunnel := cloudflare.New(options)
	s.access.Lock()
	if s.tunnelSequence == sequence {
		s.tunnel = tunnel
		tunnel = nil
	}
	s.access.Unlock()
	if tunnel != nil {
		_ = tunnel.Close()
	}
}

func (s *settingsService) cloudflareState(sequence uint64, mode cloudflare.Mode, state string, hostname string) {
	s.access.Lock()
	if sequence != s.tunnelSequence {
		s.access.Unlock()
		return
	}
	if mode == cloudflare.ModeQuickTunnel {
		s.settings.Cloudflare.QuickTunnel, s.settings.Cloudflare.QuickHostname = state, hostname
	} else {
		s.settings.Cloudflare.NamedTunnel = state
	}
	err := s.saveLocked()
	s.access.Unlock()
	if err != nil {
		s.logger.Error(E.Cause(err, "save the Cloudflare tunnel"))
	}
	s.updateStatus(func(status *proto.PublicationStatus) {
		status.CloudflareHostname = hostname
		status.CloudflareError = ""
	})
}

func (s *settingsService) cloudflareFailed(sequence uint64, err error) {
	s.access.Lock()
	current := sequence == s.tunnelSequence
	s.access.Unlock()
	if current {
		s.updateStatus(func(status *proto.PublicationStatus) { status.CloudflareError = err.Error() })
	}
}

func (s *settingsService) reconcileTailscale(enabled bool) {
	s.access.Lock()
	applied := s.tailscaleActive != nil && *s.tailscaleActive == enabled
	s.tailscaleActive = &enabled
	s.access.Unlock()
	if applied {
		return
	}
	err := s.core.tailscale.SetEnabled(enabled)
	s.updateStatus(func(status *proto.PublicationStatus) {
		status.TailscaleError = ""
		if err != nil {
			status.TailscaleError = err.Error()
		}
	})
}

func (s *settingsService) tailscaleStatus(node tailscale.Status) {
	s.updateStatus(func(status *proto.PublicationStatus) {
		status.Tailscale = &proto.TailscaleStatus{
			State: proto.TailscaleState(node.State), LoginName: node.LoginName, Tailnet: node.Tailnet, DnsName: node.DNSName,
			Addresses: common.Map(node.Addresses, netip.Addr.String),
		}
	})
}

func (s *settingsService) tailscaleFailed(err error) {
	s.updateStatus(func(status *proto.PublicationStatus) { status.TailscaleError = err.Error() })
}

func (s *settingsService) publishFailed(network string, err error) {
	s.logger.Error(E.Cause(err, "serve the API on ", network))
	s.updateStatus(func(status *proto.PublicationStatus) {
		switch network {
		case api.NetworkCloudflare:
			status.CloudflareError = err.Error()
		case api.NetworkTailscale:
			status.TailscaleError = err.Error()
		default:
			status.LocalNetwork = false
			status.LocalNetworkError = err.Error()
		}
	})
	if network == api.NetworkLocal {
		s.advertise(localNetwork{}, false)
	}
}

func (s *settingsService) LogInCloudflare(_ *emptypb.Empty, stream grpc.ServerStreamingServer[proto.LogInCloudflareResponse]) error {
	login := cloudflare.NewLogin()
	defer login.Cancel()
	go func() {
		<-stream.Context().Done()
		login.Cancel()
	}()
	err := stream.Send(&proto.LogInCloudflareResponse{Step: &proto.LogInCloudflareResponse_Url{Url: login.URL()}})
	if err != nil {
		return err
	}
	account, err := login.Wait()
	if err != nil {
		return status.Error(codes.Unavailable, err.Error())
	}
	credential, err := json.Marshal(account)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(stream.Context(), zoneLookupTimeout)
	defer cancel()
	zone, err := account.Zone(ctx)
	if err != nil {
		return status.Error(codes.Unavailable, err.Error())
	}
	return stream.Send(&proto.LogInCloudflareResponse{Step: &proto.LogInCloudflareResponse_Account{Account: &proto.CloudflareAccount{
		Credential: string(credential), Name: zone.Account.Name, Zone: zone.Name,
	}}})
}

func (s *settingsService) LogInTailscale(_ *emptypb.Empty, stream grpc.ServerStreamingServer[proto.LogInTailscaleResponse]) error {
	login, err := s.core.tailscale.Login()
	if err != nil {
		return status.Error(codes.Unavailable, err.Error())
	}
	defer login.Cancel()
	go func() {
		<-stream.Context().Done()
		login.Cancel()
	}()
	if err = stream.Send(&proto.LogInTailscaleResponse{Step: &proto.LogInTailscaleResponse_Url{Url: login.URL()}}); err != nil {
		return err
	}
	if err = login.Wait(); err != nil {
		return status.Error(codes.Unavailable, err.Error())
	}
	s.reconcile()
	return stream.Send(&proto.LogInTailscaleResponse{Step: &proto.LogInTailscaleResponse_Completed{Completed: &emptypb.Empty{}}})
}

func (s *settingsService) LogOutTailscale(context.Context, *emptypb.Empty) (*emptypb.Empty, error) {
	if err := s.core.tailscale.Logout(); err != nil {
		return nil, status.Error(codes.Unavailable, err.Error())
	}
	return &emptypb.Empty{}, nil
}

func (s *settingsService) close() {
	s.access.Lock()
	tunnel := s.tunnel
	s.tunnel, s.tunnelConfig = nil, nil
	s.tunnelSequence++
	s.access.Unlock()
	if tunnel != nil {
		_ = tunnel.Close()
	}
	_ = s.core.tailscale.SetEnabled(false)
	s.advertise(localNetwork{}, false)
}
