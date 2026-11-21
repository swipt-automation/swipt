//go:build cgo

package automation

import (
	"cmp"
	"encoding/json"
	"os"
	"path"
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
	ID              string
	Name            string
	LocalizedNames  map[string]string
	Description     string
	Version         string
	VersionCode     int64
	Icon            Icon
	MinimumAPILevel int64
	Service         bool
	Actions         []ActionInfo
	Platforms       map[OS]Platform
}

type Platform struct {
	MinimumVersion string
	MinimumSDK     int64
	Sources        string
}

const platformsAPILevel = 4

func (m Manifest) Symbol(target OS) string {
	return cmp.Or(m.Icon.Symbol(target), defaultSymbols[target])
}

type manifestFields struct {
	ID                string                `json:"id"`
	Name              string                `json:"name"`
	LocalizedNames    map[string]string     `json:"localizedNames"`
	Description       string                `json:"description"`
	Version           string                `json:"version"`
	VersionCode       int64                 `json:"versionCode"`
	Icon              Icon                  `json:"icon"`
	MinimumAPILevel   int64                 `json:"minimumAPILevel"`
	MinimumIOSVersion json.Number           `json:"minimumIOSVersion"`
	Service           bool                  `json:"service"`
	Actions           []ActionInfo          `json:"actions"`
	Platforms         map[OS]platformFields `json:"platforms"`
}

type platformFields struct {
	MinimumVersion string `json:"minimumVersion"`
	MinimumSDK     int64  `json:"minimumSDK"`
}

func ParseManifest(data []byte) (Manifest, error) {
	known := manifestFields{Icon: defaultIcon, MinimumAPILevel: 1}
	if err := json.Unmarshal(data, &known); err != nil {
		return Manifest{}, E.Cause(err, "read ", ManifestFile)
	}
	var platforms map[OS]Platform
	var err error
	switch {
	case len(known.Platforms) > 0:
		platforms, err = parsePlatforms(known.Platforms)
	case known.MinimumAPILevel < platformsAPILevel:
		platforms, err = parseLegacyIOS(known.MinimumIOSVersion)
	default:
		err = E.New("it declares no platforms")
	}
	if err != nil {
		return Manifest{}, E.Cause(err, "read ", ManifestFile)
	}
	return Manifest{
		ID: known.ID, Name: known.Name, LocalizedNames: known.LocalizedNames, Description: known.Description, Version: known.Version,
		VersionCode: known.VersionCode, Icon: known.Icon, MinimumAPILevel: known.MinimumAPILevel,
		Service: known.Service, Actions: known.Actions, Platforms: platforms,
	}, nil
}

func parsePlatforms(declared map[OS]platformFields) (map[OS]Platform, error) {
	platforms := make(map[OS]Platform)
	for target, fields := range declared {
		platform := Platform{Sources: path.Join(SourceFolder, string(target))}
		switch target {
		case IOS:
			if _, isVersion := version.Parse(fields.MinimumVersion); !isVersion {
				return nil, E.New("the minimum iOS version ", strconv.Quote(fields.MinimumVersion), " is not a version")
			}
			platform.MinimumVersion = fields.MinimumVersion
		case Android:
			if fields.MinimumSDK < 1 {
				return nil, E.New("the minimum Android SDK ", fields.MinimumSDK, " is not an SDK level")
			}
			platform.MinimumSDK = fields.MinimumSDK
		default:
			continue
		}
		platforms[target] = platform
	}
	return platforms, nil
}

func parseLegacyIOS(minimumIOSVersion json.Number) (map[OS]Platform, error) {
	var minimum string
	if minimumIOSVersion != "" {
		number, err := minimumIOSVersion.Float64()
		minimum = strconv.FormatFloat(number, 'f', -1, 64)
		if parsed, isVersion := version.Parse(minimum); err != nil || !isVersion || len(parsed) > 2 {
			return nil, E.New("the minimum iOS version ", minimumIOSVersion.String(), " is not a version")
		}
	}
	return map[OS]Platform{IOS: {MinimumVersion: minimum, Sources: SourceFolder}}, nil
}

func LoadManifest(directory string) (Manifest, error) {
	data, err := os.ReadFile(filepath.Join(directory, ManifestFile))
	if err != nil {
		return Manifest{}, err
	}
	return ParseManifest(data)
}
