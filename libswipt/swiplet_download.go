package libSwipt

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"time"

	E "github.com/sagernet/sing/common/exceptions"
	"github.com/swipt-automation/swipt/internal/automation"
)

const (
	fetchLimit         = 4 << 20
	swipletIdleTimeout = 15 * time.Second
)

var swipletClient = func() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = func(ctx context.Context, network string, address string) (net.Conn, error) {
		conn, err := (&net.Dialer{Timeout: swipletIdleTimeout}).DialContext(ctx, network, address)
		if err != nil {
			return nil, err
		}
		return &idleConn{conn}, nil
	}
	transport.TLSHandshakeTimeout = swipletIdleTimeout
	return &http.Client{Transport: transport}
}()

type idleConn struct {
	net.Conn
}

func (c *idleConn) Read(p []byte) (int, error) {
	if err := c.SetReadDeadline(time.Now().Add(swipletIdleTimeout)); err != nil {
		return 0, err
	}
	return c.Conn.Read(p)
}

func (c *idleConn) Write(p []byte) (int, error) {
	if err := c.SetWriteDeadline(time.Now().Add(swipletIdleTimeout)); err != nil {
		return 0, err
	}
	return c.Conn.Write(p)
}

const (
	SwipletErrorNone int32 = iota
	SwipletErrorFailed
	SwipletErrorOutdated
	SwipletErrorIOSOutdated
	SwipletErrorSwipletOutdated
)

type SwipletDownloader struct {
	ctx    context.Context
	cancel context.CancelFunc
}

func NewSwipletDownloader() *SwipletDownloader {
	ctx, cancel := context.WithCancel(context.Background())
	return &SwipletDownloader{ctx: ctx, cancel: cancel}
}

func (d *SwipletDownloader) Cancel() {
	d.cancel()
}

type SwipletDownloadHandler interface {
	OnProgress(received int64, total int64)
}

func (d *SwipletDownloader) Download(link string, destination string, checksum string, handler SwipletDownloadHandler) (string, error) {
	id, err := d.download(link, destination, checksum, handler)
	if err != nil {
		return "", E.Cause(err, "download ", link)
	}
	return id, nil
}

func (d *SwipletDownloader) download(link string, destination string, checksum string, handler SwipletDownloadHandler) (string, error) {
	response, err := d.get(link, http.Header{})
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", E.New(response.Status)
	}
	file, err := os.Create(destination)
	if err != nil {
		return "", err
	}
	hash := sha256.New()
	size, err := io.Copy(io.MultiWriter(file, hash, &progressWriter{handler: handler, total: response.ContentLength}), response.Body)
	if err == nil && checksum != "" && !strings.EqualFold(hex.EncodeToString(hash.Sum(nil)), checksum) {
		err = E.New("the download doesn't match the checksum")
	}
	var contents automation.Archive
	if err == nil {
		var reader *zip.Reader
		reader, err = zip.NewReader(file, size)
		if err == nil {
			contents, err = automation.ReadArchive(reader)
		} else {
			err = E.Cause(err, "open the archive")
		}
	}
	if err = E.Errors(err, file.Close()); err != nil {
		os.Remove(destination)
		return "", err
	}
	return contents.Manifest.ID, nil
}

type progressWriter struct {
	handler  SwipletDownloadHandler
	received int64
	total    int64
}

func (w *progressWriter) Write(p []byte) (int, error) {
	w.received += int64(len(p))
	w.handler.OnProgress(w.received, w.total)
	return len(p), nil
}

type Repository struct {
	EntityTag      string
	NotModified    bool
	Name           string
	Icon           *SwipletIcon
	Image          []byte
	localizedNames []*LocalizedName
	swiplets       []*RepositorySwiplet
}

func (r *Repository) LocalizedNames() LocalizedNameIterator {
	return newIterator(r.localizedNames)
}

func (r *Repository) Swiplets() RepositorySwipletIterator {
	return newIterator(r.swiplets)
}

type RepositorySwiplet struct {
	ID                string
	Name              string
	Summary           string
	Version           string
	VersionCode       int64
	MinimumAPILevel   int64
	MinimumIOSVersion string
	Color             string
	Symbol            string
	URL               string
	Checksum          string
	localizedNames    []*LocalizedName
}

func (s *RepositorySwiplet) LocalizedNames() LocalizedNameIterator {
	return newIterator(s.localizedNames)
}

type repositoryIndex struct {
	Name           string            `json:"name"`
	LocalizedNames map[string]string `json:"localizedNames"`
	Icon           *automation.Icon  `json:"icon"`
	Image          string            `json:"image"`
	Swiplets       []json.RawMessage `json:"swiplets"`
}

type repositoryEntry struct {
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
}

func (d *SwipletDownloader) FetchRepository(link string, etag string) (*Repository, error) {
	repository, err := d.fetchRepository(link, etag)
	if err != nil {
		return nil, E.Cause(err, "fetch ", link)
	}
	return repository, nil
}

func (d *SwipletDownloader) fetchRepository(link string, etag string) (*Repository, error) {
	base, err := url.Parse(link)
	if err != nil {
		return nil, err
	}
	header := http.Header{}
	if etag != "" {
		header.Set("If-None-Match", etag)
	}
	response, err := d.get(link, header)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	repository := &Repository{EntityTag: response.Header.Get("ETag")}
	switch response.StatusCode {
	case http.StatusNotModified:
		repository.NotModified = true
		return repository, nil
	case http.StatusOK:
	default:
		return nil, E.New(response.Status)
	}
	data, err := readFetched(response.Body)
	if err != nil {
		return nil, err
	}
	var index repositoryIndex
	if err = json.Unmarshal(data, &index); err != nil {
		return nil, E.Cause(err, "read the repository")
	}
	if index.Swiplets == nil {
		return nil, E.New("the file is not a repository")
	}
	repository.Name = index.Name
	if repository.Name == "" {
		repository.Name = repositoryName(base)
	}
	repository.localizedNames = newLocalizedNames(index.LocalizedNames)
	if index.Icon != nil && index.Icon.Symbol != "" {
		repository.Icon = &SwipletIcon{Color: index.Icon.Color, Symbol: index.Icon.Symbol}
	}
	for position, entry := range index.Swiplets {
		swiplet, err := newRepositorySwiplet(base, entry)
		if err != nil {
			return nil, E.Cause(err, "read swiplet ", position)
		}
		repository.swiplets = append(repository.swiplets, swiplet)
	}
	if image, err := base.Parse(index.Image); err == nil && index.Image != "" {
		repository.Image, _ = d.fetchImage(image.String())
	}
	return repository, nil
}

func newRepositorySwiplet(base *url.URL, data json.RawMessage) (*RepositorySwiplet, error) {
	manifest, err := automation.ParseManifest(data)
	if err != nil {
		return nil, err
	}
	if err = automation.CheckID(manifest.ID); err != nil {
		return nil, err
	}
	var entry repositoryEntry
	if err = json.Unmarshal(data, &entry); err != nil {
		return nil, err
	}
	if entry.URL == "" {
		return nil, E.New("the swiplet ", manifest.ID, " has no URL")
	}
	link, err := base.Parse(entry.URL)
	if err != nil {
		return nil, err
	}
	return &RepositorySwiplet{
		ID: manifest.ID, Name: manifest.Name, Summary: manifest.Description, Version: manifest.Version, VersionCode: manifest.VersionCode,
		MinimumAPILevel: manifest.MinimumAPILevel, MinimumIOSVersion: manifest.MinimumIOSVersion, Color: manifest.Icon.Color,
		Symbol: manifest.Icon.Symbol, URL: link.String(), Checksum: entry.SHA256, localizedNames: newLocalizedNames(manifest.LocalizedNames),
	}, nil
}

func repositoryName(link *url.URL) string {
	directory := path.Dir(link.Path)
	if directory == "/" || directory == "." {
		return link.Hostname()
	}
	return link.Hostname() + directory
}

func (d *SwipletDownloader) fetchImage(link string) ([]byte, error) {
	response, err := d.get(link, http.Header{})
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, E.New(response.Status)
	}
	return readFetched(response.Body)
}

func readFetched(reader io.Reader) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(reader, fetchLimit+1))
	if err != nil {
		return nil, err
	}
	if len(data) > fetchLimit {
		return nil, E.New("the file is larger than ", fetchLimit>>20, " MB")
	}
	return data, nil
}

func (d *SwipletDownloader) get(link string, header http.Header) (*http.Response, error) {
	request, err := http.NewRequestWithContext(d.ctx, http.MethodGet, link, nil)
	if err != nil {
		return nil, err
	}
	if request.URL.Scheme != "http" && request.URL.Scheme != "https" {
		return nil, E.New("unsupported URL")
	}
	request.Header = header
	response, err := swipletClient.Do(request)
	if urlError, isURL := errors.AsType[*url.Error](err); isURL {
		return nil, urlError.Err
	}
	return response, err
}
