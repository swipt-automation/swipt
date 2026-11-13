//go:build cgo

package automation

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"

	E "github.com/sagernet/sing/common/exceptions"
	"github.com/swipt-automation/swipt/internal/version"
)

type ActionInfo struct {
	Name        string          `json:"name"`
	Title       string          `json:"title"`
	Description string          `json:"description,omitempty"`
	Expose      []Caller        `json:"expose,omitempty"`
	Stream      bool            `json:"stream,omitempty"`
	Input       json.RawMessage `json:"input,omitempty"`
	Output      json.RawMessage `json:"output,omitempty"`
}

type Manifest struct {
	ID                string
	Name              string
	LocalizedNames    map[string]string
	Description       string
	Version           string
	VersionCode       int64
	Icon              Icon
	MinimumAPILevel   int64
	MinimumIOSVersion string
	Service           bool
	Actions           []ActionInfo
}

type manifestFields struct {
	ID                string            `json:"id"`
	Name              string            `json:"name"`
	LocalizedNames    map[string]string `json:"localizedNames"`
	Description       string            `json:"description"`
	Version           string            `json:"version"`
	VersionCode       int64             `json:"versionCode"`
	Icon              Icon              `json:"icon"`
	MinimumAPILevel   int64             `json:"minimumAPILevel"`
	MinimumIOSVersion json.Number       `json:"minimumIOSVersion"`
	Service           bool              `json:"service"`
	Actions           []ActionInfo      `json:"actions"`
}

func ParseManifest(data []byte) (Manifest, error) {
	known := manifestFields{Icon: DefaultIcon, MinimumAPILevel: 1}
	if err := json.Unmarshal(data, &known); err != nil {
		return Manifest{}, E.Cause(err, "read ", ManifestFile)
	}
	if known.Icon.Symbol == "" {
		known.Icon = DefaultIcon
	}
	var minimumIOSVersion string
	if known.MinimumIOSVersion != "" {
		number, err := known.MinimumIOSVersion.Float64()
		minimumIOSVersion = strconv.FormatFloat(number, 'f', -1, 64)
		if parsed, isVersion := version.Parse(minimumIOSVersion); err != nil || !isVersion || len(parsed) > 2 {
			return Manifest{}, E.New("read ", ManifestFile, ": the minimum iOS version ", known.MinimumIOSVersion.String(), " is not a version")
		}
	}
	return Manifest{
		ID: known.ID, Name: known.Name, LocalizedNames: known.LocalizedNames, Description: known.Description, Version: known.Version,
		VersionCode: known.VersionCode, Icon: known.Icon, MinimumAPILevel: known.MinimumAPILevel,
		MinimumIOSVersion: minimumIOSVersion, Service: known.Service, Actions: known.Actions,
	}, nil
}

func LoadManifest(directory string) (Manifest, error) {
	data, err := os.ReadFile(filepath.Join(directory, ManifestFile))
	if err != nil {
		return Manifest{}, err
	}
	return ParseManifest(data)
}
