package libSwipt

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"

	"github.com/sagernet/sing/common"
	E "github.com/sagernet/sing/common/exceptions"
	"github.com/swipt-automation/swipt/internal/automation"
)

const UpdateGuide = automation.UpdateGuide

var archiveMagic = []byte("PK\x03\x04")

type SwipletIcon struct {
	Color  string
	Symbol string
}

func LoadSwipletIcon(directory string) *SwipletIcon {
	icon := automation.LoadIcon(directory)
	return &SwipletIcon{Color: icon.Color, Symbol: icon.Symbol}
}

type LocalizedName struct {
	Language string
	Name     string
}

func newLocalizedNames(names map[string]string) []*LocalizedName {
	var localized []*LocalizedName
	for language, name := range names {
		localized = append(localized, &LocalizedName{Language: language, Name: name})
	}
	return localized
}

type SwipletAction struct {
	Name    string
	Title   string
	Summary string
	Stream  bool
	expose  []automation.Caller
}

func (a *SwipletAction) Exposed(caller string) bool {
	return slices.Contains(a.expose, automation.Caller(caller))
}

type SwipletManifest struct {
	ID             string
	Name           string
	Color          string
	Symbol         string
	Version        string
	VersionCode    int64
	Service        bool
	localizedNames []*LocalizedName
	actions        []*SwipletAction
}

func (m *SwipletManifest) LocalizedNames() LocalizedNameIterator {
	return newIterator(m.localizedNames)
}

func (m *SwipletManifest) Actions() SwipletActionIterator {
	return newIterator(m.actions)
}

func newSwipletManifest(manifest automation.Manifest) *SwipletManifest {
	return &SwipletManifest{
		ID: manifest.ID, Name: manifest.Name, Color: manifest.Icon.Color, Symbol: manifest.Icon.Symbol, Version: manifest.Version,
		VersionCode: manifest.VersionCode, Service: manifest.Service, localizedNames: newLocalizedNames(manifest.LocalizedNames),
		actions: common.Map(manifest.Actions, func(action automation.ActionInfo) *SwipletAction {
			return &SwipletAction{Name: action.Name, Title: action.Title, Summary: action.Description, Stream: action.Stream, expose: action.Expose}
		}),
	}
}

func ReadSwipletManifest(file string) (*SwipletManifest, error) {
	archive, err := readSwipletArchive(file)
	if err != nil {
		return nil, err
	}
	return newSwipletManifest(archive.Manifest), nil
}

func readSwipletArchive(file string) (automation.Archive, error) {
	content, err := os.ReadFile(file)
	if err != nil {
		return automation.Archive{}, err
	}
	if !bytes.HasPrefix(content, archiveMagic) {
		return automation.Archive{}, E.New("the file is not a ZIP archive")
	}
	reader, err := zip.NewReader(bytes.NewReader(content), int64(len(content)))
	if err != nil {
		return automation.Archive{}, E.Cause(err, "open the archive")
	}
	return automation.ReadArchive(reader)
}

type SwipletInfo struct {
	ID          string
	Directory   string
	Manifest    *SwipletManifest
	NeedsUpdate bool
}

func (p *Platform) Swiplets() (SwipletInfoIterator, error) {
	swiplets, err := p.host.Swiplets()
	if err != nil {
		return nil, err
	}
	return newIterator(common.Map(swiplets, func(swiplet automation.Swiplet) *SwipletInfo {
		return &SwipletInfo{
			ID: swiplet.ID, Directory: swiplet.Directory, Manifest: newSwipletManifest(swiplet.Manifest),
			NeedsUpdate: errors.Is(p.host.Compatible(swiplet.Manifest), automation.ErrSwipletOutdated),
		}
	})), nil
}

type SwipletInstall struct {
	ID string

	ErrorCode         int32
	ErrorMessage      string
	MinimumIOSVersion string
}

func (p *Platform) InstallSwiplet(file string) *SwipletInstall {
	id, err := p.installSwiplet(file)
	return newSwipletInstall(id, err)
}

func (p *Platform) SwipletCompatible(minimumAPILevel int64, minimumIOSVersion string) bool {
	return p.host.Compatible(automation.Manifest{MinimumAPILevel: minimumAPILevel, MinimumIOSVersion: minimumIOSVersion}) == nil
}

func newSwipletInstall(id string, err error) *SwipletInstall {
	var outdatedIOS *automation.IOSOutdatedError
	switch {
	case err == nil:
		return &SwipletInstall{ID: id}
	case errors.Is(err, automation.ErrOutdated):
		return &SwipletInstall{ErrorCode: SwipletErrorOutdated, ErrorMessage: err.Error()}
	case errors.Is(err, automation.ErrSwipletOutdated):
		return &SwipletInstall{ErrorCode: SwipletErrorSwipletOutdated, ErrorMessage: err.Error()}
	case errors.As(err, &outdatedIOS):
		return &SwipletInstall{ErrorCode: SwipletErrorIOSOutdated, ErrorMessage: err.Error(), MinimumIOSVersion: outdatedIOS.Minimum}
	default:
		return &SwipletInstall{ErrorCode: SwipletErrorFailed, ErrorMessage: err.Error()}
	}
}

func (p *Platform) installSwiplet(file string) (string, error) {
	archive, err := os.Open(file)
	if err != nil {
		return "", err
	}
	defer archive.Close()
	info, err := archive.Stat()
	if err != nil {
		return "", err
	}
	swiplet, err := p.host.Install(archive, info.Size())
	return swiplet.ID, err
}

func (p *Platform) RemoveSwiplet(swiplet string) error {
	return p.host.Remove(swiplet)
}

func (p *Platform) ClearSwipletData(swiplet string) error {
	return p.host.ClearData(swiplet)
}

func (p *Platform) ExportSwiplet(swiplet string, destination string) error {
	installed, err := p.host.Swiplet(swiplet)
	if err != nil {
		return err
	}
	names := []string{automation.ManifestFile}
	if _, err = os.Stat(filepath.Join(installed.Directory, automation.IconFile)); err == nil {
		names = append(names, automation.IconFile)
	}
	for _, folder := range []string{automation.SourceFolder, automation.AssetsFolder} {
		if _, err = os.Stat(filepath.Join(installed.Directory, folder)); errors.Is(err, fs.ErrNotExist) {
			continue
		}
		err = fs.WalkDir(os.DirFS(installed.Directory), folder, func(name string, item fs.DirEntry, err error) error {
			if err == nil && item.Type().IsRegular() {
				names = append(names, name)
			}
			return err
		})
		if err != nil {
			return err
		}
	}
	file, err := os.Create(destination)
	if err != nil {
		return err
	}
	writer := zip.NewWriter(file)
	err = writeArchive(writer, installed.Directory, names)
	return E.Errors(err, writer.Close(), file.Close())
}

func writeArchive(writer *zip.Writer, directory string, names []string) error {
	for _, name := range names {
		info, err := os.Stat(filepath.Join(directory, name))
		if err != nil {
			return err
		}
		header, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		header.Name, header.Method = name, zip.Deflate
		output, err := writer.CreateHeader(header)
		if err != nil {
			return err
		}
		if err = copyFile(output, filepath.Join(directory, name)); err != nil {
			return err
		}
	}
	return nil
}

func copyFile(writer io.Writer, name string) error {
	file, err := os.Open(name)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = io.Copy(writer, file)
	return err
}

func (p *Platform) SwipletDatabase(client, swiplet, request string) (string, error) {
	result, err := p.host.SQLite(context.Background(), client, swiplet, []byte(request))
	if err != nil {
		return "", err
	}
	data, err := json.Marshal(result)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (p *Platform) CloseSwipletDatabases(client string) {
	p.host.CloseDatabases(client, "")
}
