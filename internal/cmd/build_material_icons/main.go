package main

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

const (
	fontAsset       = "android/app/src/main/assets/MaterialIcons-Regular.ttf"
	codepointsAsset = "android/app/src/main/assets/MaterialIcons-Regular.codepoints"
	typeScriptPath  = "cli/src/material-icons.ts"

	releaseBase      = "https://raw.githubusercontent.com/google/material-design-icons/10ecc14790612ca8fd164e3f4ffe9c16fc50d5b7/font/"
	fontURL          = releaseBase + "MaterialIcons-Regular.ttf"
	fontSHA256       = "ef149f08bdd2ff09a4e2c8573476b7b0f3fbb15b623954ade59899e7175bedda"
	codepointsURL    = releaseBase + "MaterialIcons-Regular.codepoints"
	codepointsSHA256 = "530f25bf7b2d71c8e1da9476d53f9a9bb6b7e187bff69bb7128bb679b8194894"
)

func main() {
	font := fetch(fontURL, fontSHA256)
	codepoints := fetch(codepointsURL, codepointsSHA256)

	icons := make(map[string]string)
	scanner := bufio.NewScanner(bytes.NewReader(codepoints))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		name, code, found := strings.Cut(line, " ")
		if !found {
			log.Fatal("invalid codepoints line: ", line)
		}
		if _, err := strconv.ParseUint(code, 16, 32); err != nil {
			log.Fatal(name, ": ", err)
		}
		icons[name] = code
	}
	if err := scanner.Err(); err != nil {
		log.Fatal(err)
	}

	names := slices.Sorted(maps.Keys(icons))
	var normalized strings.Builder
	for _, name := range names {
		normalized.WriteString(name)
		normalized.WriteByte(' ')
		normalized.WriteString(icons[name])
		normalized.WriteByte('\n')
	}

	write(fontAsset, font)
	write(codepointsAsset, []byte(normalized.String()))
	write(typeScriptPath, []byte(typeScriptSet(names)))
	log.Print(len(names), " Material icons")
}

func fetch(url string, expectedSHA256 string) []byte {
	log.Print("downloading ", url)
	response, err := http.Get(url)
	if err != nil {
		log.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		log.Fatal(url, ": ", response.Status)
	}
	content, err := io.ReadAll(response.Body)
	if err != nil {
		log.Fatal(url, ": ", err)
	}
	sum := sha256.Sum256(content)
	if actual := hex.EncodeToString(sum[:]); actual != expectedSHA256 {
		log.Fatal(url, ": sha256 is ", actual, ", not ", expectedSHA256)
	}
	return content
}

func typeScriptSet(names []string) string {
	var output strings.Builder
	output.WriteString("export const materialIcons: ReadonlySet<string> = new Set([\n")
	for _, name := range names {
		fmt.Fprintf(&output, "  %q,\n", name)
	}
	output.WriteString("]);\n")
	return output.String()
}

func write(file string, content []byte) {
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile(file, content, 0o644); err != nil {
		log.Fatal(err)
	}
	log.Print("wrote ", file)
}
