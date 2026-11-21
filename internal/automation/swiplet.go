//go:build cgo

package automation

import (
	"cmp"
	"encoding/json"
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

var (
	defaultIcon    = Icon{Color: "brown"}
	defaultSymbols = map[OS]string{IOS: "square.2.layers.3d.fill", Android: "layers"}
)

type Icon struct {
	Color       string
	SymbolColor string
	Symbols     map[OS]string
}

func (i Icon) Symbol(target OS) string {
	return i.Symbols[target]
}

func (i *Icon) UnmarshalJSON(data []byte) error {
	var fields struct {
		Color       string `json:"color"`
		SymbolColor string `json:"symbolColor"`
		IOS         string `json:"ios"`
		Android     string `json:"android"`
		Symbol      string `json:"symbol"`
	}
	fields.Color, fields.SymbolColor = i.Color, i.SymbolColor
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	symbols := make(map[OS]string)
	if symbol := cmp.Or(fields.IOS, fields.Symbol); symbol != "" {
		symbols[IOS] = symbol
	}
	if fields.Android != "" {
		symbols[Android] = fields.Android
	}
	*i = Icon{Color: fields.Color, SymbolColor: fields.SymbolColor, Symbols: symbols}
	return nil
}

func LoadIcon(directory string, target OS) (Icon, string) {
	manifest, err := LoadManifest(directory)
	if err != nil {
		manifest.Icon = defaultIcon
	}
	return manifest.Icon, manifest.Symbol(target)
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
