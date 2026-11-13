//go:build cgo

package automation

import (
	"strconv"

	E "github.com/sagernet/sing/common/exceptions"
)

const (
	SourceFolder  = "src"
	AssetsFolder  = "assets"
	DataFolder    = "data"
	InterfaceFile = "app.js"
	ExtensionFile = "extension.js"
	ExtensionMap  = "extension.js.map"
	ManifestFile  = "manifest.json"
	IconFile      = "icon.png"
	serviceFile   = ".service"
	longestID     = 128
)

var DefaultIcon = Icon{Color: "brown", Symbol: "square.2.layers.3d.fill"}

type Icon struct {
	Color       string `json:"color"`
	Symbol      string `json:"symbol"`
	SymbolColor string `json:"symbolColor"`
}

func LoadIcon(directory string) Icon {
	manifest, err := LoadManifest(directory)
	if err != nil {
		return DefaultIcon
	}
	return manifest.Icon
}

func CheckID(id string) error {
	valid := id != "" && len(id) <= longestID
	for index, char := range id {
		switch {
		case char >= 'a' && char <= 'z', char >= '0' && char <= '9':
		case index > 0 && (char == '.' || char == '_' || char == '-'):
		default:
			valid = false
		}
	}
	if !valid {
		return E.Cause1(ErrInvalid, E.New("the swiplet ID ", strconv.Quote(id), " is not 1 to ", longestID, " lowercase letters, digits, dots, underscores and hyphens starting with a letter or a digit"))
	}
	return nil
}
