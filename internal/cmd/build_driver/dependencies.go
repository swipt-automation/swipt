package main

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
)

type download struct {
	url    string
	sha256 string
}

// The WebKit of a Bun release (oven-sh/bun v1.4.4, scripts/build/deps/webkit.ts:6),
// with the dependencies its prebuilt archives leave to Bun at the commits Bun
// builds them from (scripts/build/deps/mimalloc.ts:15, scripts/build/deps/zstd.ts:15).
const webKitRelease = "https://github.com/oven-sh/WebKit/releases/download/autobuild-d6375c72affe0f70222cdcb45012b10f0bf3a9d5/"

var (
	webKit = map[target]download{
		{"linux", "amd64"}:   pin(webKitRelease+"bun-webkit-linux-amd64.tar.gz", "e6a0a23bb91d19ec39d3331ed997ad67e156499512ee30a4e0f3fd9e64da80ea"),
		{"linux", "arm64"}:   pin(webKitRelease+"bun-webkit-linux-arm64.tar.gz", "320c101f5247e0575e3ca41b5dee7f8a2b20365f5f7a58fbd85bee8d63d0e598"),
		{"windows", "amd64"}: pin(webKitRelease+"bun-webkit-windows-amd64.tar.gz", "70eb12360936d3726081dd196c982a33e2f1e0b6ebfe7603696a1c49b97bd4f9"),
		{"windows", "arm64"}: pin(webKitRelease+"bun-webkit-windows-arm64.tar.gz", "adca748758acad03f1508c6fc82a858571feab81b17af19db68436a428da16c6"),
	}
	mimalloc = pin("https://github.com/oven-sh/mimalloc/archive/92ef6587c57b262f6e6abf513f7776a2f8d12aeb.tar.gz", "1e708b1730b96b21b9daad43cd67b10cb4af138a73daf549db5fdc0caac69510")
	zstd     = pin("https://github.com/facebook/zstd/archive/f8745da6ff1ad1e7bab384bd1f9d742439278e99.tar.gz", "4b0bd1f0cfb25e61b9103c35f27395530ff5b4c0d2513a00fd745849e85ea52c")
	sqlite   = pin("https://www.sqlite.org/2026/sqlite-amalgamation-3540100.zip", "0a9c8e34352dad46006aa82836d5383bff9dc6bc9ce5a60553c05aa30e6ad1f9")
)

var pinned []download

func pin(url string, checksum string) download {
	source := download{url, checksum}
	pinned = append(pinned, source)
	return source
}

func (d download) file() string {
	return filepath.Join(buildDirectory, "downloads", d.sha256[:16]+"-"+path.Base(d.url))
}

func pruneDownloads() {
	kept := make(map[string]bool)
	for _, source := range pinned {
		kept[filepath.Base(source.file())] = true
	}
	directory := filepath.Join(buildDirectory, "downloads")
	entries, err := os.ReadDir(directory)
	if err != nil && !os.IsNotExist(err) {
		log.Fatal(err)
	}
	for _, entry := range entries {
		if !kept[entry.Name()] && !strings.HasPrefix(entry.Name(), ".") {
			if err = os.Remove(filepath.Join(directory, entry.Name())); err != nil {
				log.Fatal(err)
			}
		}
	}
}

func fetch(source download) string {
	file := source.file()
	if _, err := os.Stat(file); err == nil {
		return file
	}
	log.Print("downloading ", source.url)
	response, err := http.Get(source.url)
	if err != nil {
		log.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		log.Fatal(source.url, ": ", response.Status)
	}
	if err = os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		log.Fatal(err)
	}
	partial, err := os.CreateTemp(filepath.Dir(file), ".download-*")
	if err != nil {
		log.Fatal(err)
	}
	defer os.Remove(partial.Name())
	hash := sha256.New()
	_, err = io.Copy(io.MultiWriter(partial, hash), response.Body)
	if closeErr := partial.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		log.Fatal(source.url, ": ", err)
	}
	if sum := hex.EncodeToString(hash.Sum(nil)); sum != source.sha256 {
		log.Fatal(source.url, ": sha256 is ", sum, ", not ", source.sha256)
	}
	if err = os.Rename(partial.Name(), file); err != nil {
		log.Fatal(err)
	}
	return file
}

func unpack(source download, directory string, arguments ...string) string {
	return prepare(directory, digest(append([]string{source.sha256}, arguments...)...), func(partial string) {
		archive := fetch(source)
		if isZip(archive) {
			unzip(archive, partial)
		} else {
			run(exec.Command("tar", append([]string{"-xf", archive, "-C", partial}, arguments...)...))
		}
	})
}

func digest(inputs ...string) string {
	hash := sha256.New()
	for _, input := range inputs {
		hash.Write([]byte(input))
		hash.Write([]byte{0})
	}
	return hex.EncodeToString(hash.Sum(nil))[:16]
}

func prepare(directory string, key string, create func(partial string)) string {
	prepared := directory + "-" + key
	if _, err := os.Stat(prepared); err == nil {
		return prepared
	}
	parent, name := filepath.Split(directory)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		log.Fatal(err)
	}
	partial, err := os.MkdirTemp(parent, "."+name+"-")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(partial)
	create(partial)
	if err = os.Rename(partial, prepared); err != nil {
		if _, statErr := os.Stat(prepared); statErr != nil {
			log.Fatal(err)
		}
		return prepared
	}
	stale, err := filepath.Glob(filepath.Join(parent, name+"-"+strings.Repeat("[0-9a-f]", len(key))))
	if err != nil {
		log.Fatal(err)
	}
	for _, entry := range stale {
		if entry != prepared {
			if err = os.RemoveAll(entry); err != nil {
				log.Fatal(err)
			}
		}
	}
	return prepared
}

func debData(file string, directory string) {
	content, err := os.ReadFile(file)
	if err != nil {
		log.Fatal(err)
	}
	const magic = "!<arch>\n"
	if !bytes.HasPrefix(content, []byte(magic)) {
		log.Fatal(file, ": not a Debian package")
	}
	for offset := len(magic); offset+60 <= len(content); {
		header := content[offset : offset+60]
		name := strings.TrimSuffix(strings.TrimSpace(string(header[:16])), "/")
		var size int
		for _, digit := range strings.TrimSpace(string(header[48:58])) {
			size = size*10 + int(digit-'0')
		}
		body := content[offset+60 : offset+60+size]
		if strings.HasPrefix(name, "data.tar") {
			member, err := os.CreateTemp("", "*-"+name)
			if err != nil {
				log.Fatal(err)
			}
			defer os.Remove(member.Name())
			_, err = member.Write(body)
			if closeErr := member.Close(); err == nil {
				err = closeErr
			}
			if err != nil {
				log.Fatal(err)
			}
			run(exec.Command("tar", "-xf", member.Name(), "-C", directory))
			return
		}
		offset += 60 + size + size%2
	}
	log.Fatal(file, ": no data member")
}

func isZip(file string) bool {
	source, err := os.Open(file)
	if err != nil {
		log.Fatal(err)
	}
	defer source.Close()
	header := make([]byte, 4)
	_, err = io.ReadFull(source, header)
	return err == nil && string(header) == "PK\x03\x04"
}

func unzip(file string, directory string) {
	reader, err := zip.OpenReader(file)
	if err != nil {
		log.Fatal(err)
	}
	defer reader.Close()
	for _, entry := range reader.File {
		_, name, found := strings.Cut(entry.Name, "/")
		if !found || name == "" || entry.FileInfo().IsDir() {
			continue
		}
		source, err := entry.Open()
		if err != nil {
			log.Fatal(err)
		}
		content, err := io.ReadAll(source)
		source.Close()
		if err != nil {
			log.Fatal(err)
		}
		file := filepath.Join(directory, filepath.FromSlash(name))
		if err = os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
			log.Fatal(err)
		}
		if err = os.WriteFile(file, content, 0o644); err != nil {
			log.Fatal(err)
		}
	}
}
