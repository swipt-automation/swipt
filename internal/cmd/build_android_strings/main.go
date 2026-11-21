package main

import (
	"cmp"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"log"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

const (
	resourcesPath = "android/app/src/main/res"
	outputName    = "strings_translated.xml"
)

var catalogPaths = []string{
	"Swipt/Resources/Localizable.xcstrings",
	"Swipt/Resources/InfoPlist.xcstrings",
	"android/app/Localizable.xcstrings",
}

var quantities = []string{"zero", "one", "two", "few", "many", "other"}

var (
	specifierPattern   = regexp.MustCompile(`%(?:(\d+)\$)?(?:\.\d+)?(@|lld|ld|d|f|s)`)
	leadingSpecPattern = regexp.MustCompile(`^%(?:(\d+)\$)?(?:\.\d+)?(@|lld|ld|d|f|s)`)
)

type catalog struct {
	SourceLanguage string                  `json:"sourceLanguage"`
	Strings        map[string]catalogEntry `json:"strings"`
}

type catalogEntry struct {
	Localizations map[string]struct {
		StringUnit stringUnit `json:"stringUnit"`
		Variations struct {
			Plural map[string]struct {
				StringUnit stringUnit `json:"stringUnit"`
			} `json:"plural"`
		} `json:"variations"`
	} `json:"localizations"`
}

type stringUnit struct {
	Value string `json:"value"`
}

type forms map[string]string

type baseString struct {
	name   string
	plural bool
	forms  forms
}

func main() {
	translations := make(map[string]map[string]forms)
	canonicalKeys := make(map[string]string)
	for _, path := range catalogPaths {
		content, err := os.ReadFile(path)
		if err != nil {
			log.Fatal(err)
		}
		var loaded catalog
		err = json.Unmarshal(content, &loaded)
		if err != nil {
			log.Fatal(path, ": ", err)
		}
		for _, key := range slices.Sorted(maps.Keys(loaded.Strings)) {
			entry := loaded.Strings[key]
			source := key
			if english, found := entry.Localizations[loaded.SourceLanguage]; found {
				source = cmp.Or(english.StringUnit.Value, english.Variations.Plural["other"].StringUnit.Value, key)
			}
			canonical := canonicalize(source)
			if _, found := canonicalKeys[canonical]; !found || source == key {
				canonicalKeys[canonical] = key
			}
			for language, localization := range entry.Localizations {
				if language == loaded.SourceLanguage {
					continue
				}
				translated := make(forms)
				if localization.StringUnit.Value != "" {
					translated["other"] = localization.StringUnit.Value
				}
				for quantity, variation := range localization.Variations.Plural {
					if variation.StringUnit.Value != "" {
						translated[quantity] = variation.StringUnit.Value
					}
				}
				if translated["other"] == "" {
					continue
				}
				if translations[language] == nil {
					translations[language] = make(map[string]forms)
				}
				translations[language][key] = translated
			}
		}
	}

	base := loadBaseStrings()
	for _, language := range slices.Sorted(maps.Keys(translations)) {
		var output strings.Builder
		output.WriteString("<?xml version=\"1.0\" encoding=\"utf-8\"?>\n<resources>\n")
		count := 0
		for _, item := range base {
			key, found := canonicalKeys[canonicalize(item.forms["other"])]
			if !found {
				continue
			}
			translated, found := translations[language][key]
			if !found {
				continue
			}
			positional := strings.Contains(item.forms["other"], "$")
			formatted := specifierPattern.MatchString(item.forms["other"])
			if item.plural {
				fmt.Fprintf(&output, "    <plurals name=\"%s\">\n", item.name)
				for _, quantity := range quantities {
					if text, found := translated[quantity]; found {
						fmt.Fprintf(&output, "        <item quantity=\"%s\">%s</item>\n", quantity, escape(androidFormat(text, positional, formatted)))
					}
				}
				output.WriteString("    </plurals>\n")
			} else {
				fmt.Fprintf(&output, "    <string name=\"%s\">%s</string>\n", item.name, escape(androidFormat(translated["other"], positional, formatted)))
			}
			count++
		}
		output.WriteString("</resources>\n")
		directory := filepath.Join(resourcesPath, "values-"+qualifier(language))
		err := os.MkdirAll(directory, 0o755)
		if err != nil {
			log.Fatal(err)
		}
		err = os.WriteFile(filepath.Join(directory, outputName), []byte(output.String()), 0o644)
		if err != nil {
			log.Fatal(err)
		}
		log.Print(language, ": ", count, "/", len(base))
	}
}

func loadBaseStrings() []baseString {
	paths, err := filepath.Glob(filepath.Join(resourcesPath, "values", "strings*.xml"))
	if err != nil {
		log.Fatal(err)
	}
	var loaded []baseString
	for _, path := range paths {
		file, err := os.Open(path)
		if err != nil {
			log.Fatal(err)
		}
		decoder := xml.NewDecoder(file)
		for {
			token, err := decoder.Token()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				log.Fatal(path, ": ", err)
			}
			start, isStart := token.(xml.StartElement)
			if !isStart || start.Name.Local != "string" && start.Name.Local != "plurals" {
				continue
			}
			var element struct {
				Name         string `xml:"name,attr"`
				Translatable string `xml:"translatable,attr"`
				Text         string `xml:",chardata"`
				Items        []struct {
					Quantity string `xml:"quantity,attr"`
					Text     string `xml:",chardata"`
				} `xml:"item"`
			}
			err = decoder.DecodeElement(&element, &start)
			if err != nil {
				log.Fatal(path, ": ", err)
			}
			if element.Translatable == "false" {
				continue
			}
			item := baseString{name: element.Name, plural: start.Name.Local == "plurals", forms: forms{"other": unescape(element.Text)}}
			for _, form := range element.Items {
				item.forms[form.Quantity] = unescape(form.Text)
			}
			loaded = append(loaded, item)
		}
		file.Close()
	}
	return loaded
}

func canonicalize(text string) string {
	next := 0
	return specifierPattern.ReplaceAllStringFunc(strings.ToLower(text), func(specifier string) string {
		match := specifierPattern.FindStringSubmatch(specifier)
		position := match[1]
		if position == "" {
			next++
			position = strconv.Itoa(next)
		}
		kind := "s"
		switch match[2] {
		case "lld", "ld", "d":
			kind = "d"
		case "f":
			kind = "f"
		}
		return "%" + position + "$" + kind
	})
}

func androidFormat(text string, positional bool, formatted bool) string {
	var out strings.Builder
	next := 0
	for index := 0; index < len(text); {
		if text[index] != '%' {
			out.WriteByte(text[index])
			index++
			continue
		}
		if index+1 < len(text) && text[index+1] == '%' {
			out.WriteString("%%")
			index += 2
			continue
		}
		if match := leadingSpecPattern.FindStringSubmatch(text[index:]); match != nil {
			specifier := match[0]
			position := match[1]
			if position == "" {
				next++
				position = strconv.Itoa(next)
			}
			kind := "s"
			switch match[2] {
			case "lld", "ld", "d":
				kind = "d"
			case "f":
				out.WriteString(specifier)
				index += len(specifier)
				continue
			}
			if !positional {
				out.WriteString("%" + kind)
			} else {
				out.WriteString("%" + position + "$" + kind)
			}
			index += len(specifier)
			continue
		}
		if formatted {
			out.WriteString("%%")
		} else {
			out.WriteByte('%')
		}
		index++
	}
	return out.String()
}

func unescape(text string) string {
	var output strings.Builder
	text = strings.TrimSpace(text)
	if len(text) >= 2 && text[0] == '"' && text[len(text)-1] == '"' {
		text = text[1 : len(text)-1]
	}
	for index := 0; index < len(text); index++ {
		if text[index] != '\\' || index+1 == len(text) {
			output.WriteByte(text[index])
			continue
		}
		index++
		switch text[index] {
		case 'n':
			output.WriteByte('\n')
		case 't':
			output.WriteByte('\t')
		case 'u':
			if index+4 < len(text) {
				if code, err := strconv.ParseUint(text[index+1:index+5], 16, 32); err == nil {
					output.WriteRune(rune(code))
					index += 4
					continue
				}
			}
			output.WriteByte('u')
		default:
			output.WriteByte(text[index])
		}
	}
	return output.String()
}

func escape(text string) string {
	replacer := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		"\\", "\\\\",
		"'", "\\'",
		"\"", "\\\"",
		"\n", "\\n",
	)
	escaped := replacer.Replace(text)
	if strings.HasPrefix(escaped, "@") || strings.HasPrefix(escaped, "?") {
		escaped = "\\" + escaped
	}
	return escaped
}

func qualifier(language string) string {
	parts := strings.Split(language, "-")
	switch {
	case len(parts) == 1:
		return parts[0]
	case len(parts) == 2 && len(parts[1]) == 2:
		return parts[0] + "-r" + parts[1]
	default:
		return "b+" + strings.Join(parts, "+")
	}
}
