//go:build cgo

package repository

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
	fetchLimit  = 4 << 20
	idleTimeout = 15 * time.Second
)

var client = func() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = func(ctx context.Context, network string, address string) (net.Conn, error) {
		conn, err := (&net.Dialer{Timeout: idleTimeout}).DialContext(ctx, network, address)
		if err != nil {
			return nil, err
		}
		return &idleConn{conn}, nil
	}
	transport.TLSHandshakeTimeout = idleTimeout
	return &http.Client{Transport: transport}
}()

type idleConn struct {
	net.Conn
}

func (c *idleConn) Read(p []byte) (int, error) {
	if err := c.SetReadDeadline(time.Now().Add(idleTimeout)); err != nil {
		return 0, err
	}
	return c.Conn.Read(p)
}

func (c *idleConn) Write(p []byte) (int, error) {
	if err := c.SetWriteDeadline(time.Now().Add(idleTimeout)); err != nil {
		return 0, err
	}
	return c.Conn.Write(p)
}

type Icon struct {
	Color       string
	SymbolColor string
	Symbol      string
}

type Repository struct {
	EntityTag      string
	NotModified    bool
	Name           string
	LocalizedNames map[string]string
	Icon           *Icon
	Image          []byte
	Swiplets       []Swiplet
}

type Swiplet struct {
	ID               string
	Name             string
	LocalizedNames   map[string]string
	Description      string
	Version          string
	VersionCode      int64
	MinimumAPILevel  int64
	MinimumOSVersion string
	Icon             Icon
	URL              string
	SHA256           string
}

type index struct {
	Name           string            `json:"name"`
	LocalizedNames map[string]string `json:"localizedNames"`
	Icon           automation.Icon   `json:"icon"`
	Image          string            `json:"image"`
	Swiplets       []json.RawMessage `json:"swiplets"`
}

type entry struct {
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
}

func Fetch(ctx context.Context, link string, etag string, target automation.OS) (*Repository, error) {
	repository, err := fetch(ctx, link, etag, target)
	if err != nil {
		return nil, E.Cause(err, "fetch ", link)
	}
	return repository, nil
}

func fetch(ctx context.Context, link string, etag string, target automation.OS) (*Repository, error) {
	base, err := url.Parse(link)
	if err != nil {
		return nil, err
	}
	header := http.Header{}
	if etag != "" {
		header.Set("If-None-Match", etag)
	}
	response, err := get(ctx, link, header)
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
	var parsed index
	if err = json.Unmarshal(data, &parsed); err != nil {
		return nil, E.Cause(err, "read the repository")
	}
	if parsed.Swiplets == nil {
		return nil, E.New("the file is not a repository")
	}
	repository.Name = parsed.Name
	if repository.Name == "" {
		repository.Name = nameOf(base)
	}
	repository.LocalizedNames = parsed.LocalizedNames
	if symbol := parsed.Icon.Symbol(target); symbol != "" {
		repository.Icon = &Icon{Color: parsed.Icon.Color, SymbolColor: parsed.Icon.SymbolColor, Symbol: symbol}
	}
	for position, data := range parsed.Swiplets {
		swiplet, supported, err := swipletOf(base, data, target)
		if err != nil {
			return nil, E.Cause(err, "read swiplet ", position)
		}
		if supported {
			repository.Swiplets = append(repository.Swiplets, swiplet)
		}
	}
	if image, err := base.Parse(parsed.Image); err == nil && parsed.Image != "" {
		repository.Image, _ = fetchImage(ctx, image.String())
	}
	return repository, nil
}

func swipletOf(base *url.URL, data json.RawMessage, target automation.OS) (Swiplet, bool, error) {
	manifest, err := automation.ParseManifest(data)
	if err != nil {
		return Swiplet{}, false, err
	}
	if err = automation.CheckID(manifest.ID); err != nil {
		return Swiplet{}, false, err
	}
	platform, supported := manifest.Platforms[target]
	if !supported {
		return Swiplet{}, false, nil
	}
	var parsed entry
	if err = json.Unmarshal(data, &parsed); err != nil {
		return Swiplet{}, false, err
	}
	if parsed.URL == "" {
		return Swiplet{}, false, E.New("the swiplet ", manifest.ID, " has no URL")
	}
	link, err := base.Parse(parsed.URL)
	if err != nil {
		return Swiplet{}, false, err
	}
	return Swiplet{
		ID: manifest.ID, Name: manifest.Name, LocalizedNames: manifest.LocalizedNames, Description: manifest.Description,
		Version: manifest.Version, VersionCode: manifest.VersionCode, MinimumAPILevel: manifest.MinimumAPILevel,
		MinimumOSVersion: platform.MinimumOS(),
		Icon:             Icon{Color: manifest.Icon.Color, SymbolColor: manifest.Icon.SymbolColor, Symbol: manifest.Symbol(target)},
		URL:              link.String(), SHA256: parsed.SHA256,
	}, true, nil
}

func nameOf(link *url.URL) string {
	directory := path.Dir(link.Path)
	if directory == "/" || directory == "." {
		return link.Hostname()
	}
	return link.Hostname() + directory
}

func fetchImage(ctx context.Context, link string) ([]byte, error) {
	response, err := get(ctx, link, http.Header{})
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

func get(ctx context.Context, link string, header http.Header) (*http.Response, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, link, nil)
	if err != nil {
		return nil, err
	}
	if request.URL.Scheme != "http" && request.URL.Scheme != "https" {
		return nil, E.New("unsupported URL")
	}
	request.Header = header
	response, err := client.Do(request)
	if urlError, isURL := errors.AsType[*url.Error](err); isURL {
		return nil, urlError.Err
	}
	return response, err
}

func Download(ctx context.Context, link string, destination string, checksum string, progress func(received, total int64)) (string, error) {
	var file *os.File
	size, err := transfer(ctx, link, checksum, func(total int64) (io.Writer, error) {
		var err error
		file, err = os.Create(destination)
		if err != nil {
			return nil, err
		}
		return io.MultiWriter(file, &progressWriter{report: progress, total: total}), nil
	})
	if file == nil {
		return "", E.Cause(err, "download ", link)
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
		return "", E.Cause(err, "download ", link)
	}
	return contents.Manifest.ID, nil
}

func Transfer(ctx context.Context, link string, checksum string, open func(total int64) (io.Writer, error)) error {
	_, err := transfer(ctx, link, checksum, open)
	if err != nil {
		return E.Cause(err, "download ", link)
	}
	return nil
}

func transfer(ctx context.Context, link string, checksum string, open func(total int64) (io.Writer, error)) (int64, error) {
	response, err := get(ctx, link, http.Header{})
	if err != nil {
		return 0, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return 0, E.New(response.Status)
	}
	output, err := open(response.ContentLength)
	if err != nil {
		return 0, err
	}
	hash := sha256.New()
	size, err := io.Copy(io.MultiWriter(output, hash), response.Body)
	if err == nil && checksum != "" && !strings.EqualFold(hex.EncodeToString(hash.Sum(nil)), checksum) {
		err = E.New("the download doesn't match the checksum")
	}
	return size, err
}

type progressWriter struct {
	report   func(received, total int64)
	received int64
	total    int64
}

func (w *progressWriter) Write(p []byte) (int, error) {
	w.received += int64(len(p))
	w.report(w.received, w.total)
	return len(p), nil
}
