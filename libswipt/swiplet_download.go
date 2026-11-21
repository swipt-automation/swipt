//go:build !android

package libSwipt

import (
	"context"

	"github.com/swipt-automation/swipt/internal/repository"
)

const (
	SwipletErrorNone int32 = iota
	SwipletErrorFailed
	SwipletErrorOutdated
	SwipletErrorOSOutdated
	SwipletErrorSwipletOutdated
	SwipletErrorUnsupportedOS
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
	return repository.Download(d.ctx, link, destination, checksum, handler.OnProgress)
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
	ID               string
	Name             string
	Summary          string
	Version          string
	VersionCode      int64
	MinimumAPILevel  int64
	MinimumOSVersion string
	Icon             *SwipletIcon
	URL              string
	Checksum         string
	localizedNames   []*LocalizedName
}

func (s *RepositorySwiplet) LocalizedNames() LocalizedNameIterator {
	return newIterator(s.localizedNames)
}

func (d *SwipletDownloader) FetchRepository(link string, etag string) (*Repository, error) {
	fetched, err := repository.Fetch(d.ctx, link, etag, deviceOS)
	if err != nil {
		return nil, err
	}
	out := &Repository{
		EntityTag: fetched.EntityTag, NotModified: fetched.NotModified, Name: fetched.Name, Image: fetched.Image,
		localizedNames: newLocalizedNames(fetched.LocalizedNames),
	}
	if fetched.Icon != nil {
		out.Icon = &SwipletIcon{Color: fetched.Icon.Color, Symbol: fetched.Icon.Symbol, SymbolColor: fetched.Icon.SymbolColor}
	}
	for _, swiplet := range fetched.Swiplets {
		out.swiplets = append(out.swiplets, &RepositorySwiplet{
			ID: swiplet.ID, Name: swiplet.Name, Summary: swiplet.Description, Version: swiplet.Version, VersionCode: swiplet.VersionCode,
			MinimumAPILevel: swiplet.MinimumAPILevel, MinimumOSVersion: swiplet.MinimumOSVersion,
			Icon: &SwipletIcon{Color: swiplet.Icon.Color, Symbol: swiplet.Icon.Symbol, SymbolColor: swiplet.Icon.SymbolColor},
			URL:  swiplet.URL, Checksum: swiplet.SHA256,
			localizedNames: newLocalizedNames(swiplet.LocalizedNames),
		})
	}
	return out, nil
}
