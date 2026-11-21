package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/swipt-automation/swipt/internal/cmd/toolchain"
)

const (
	driverPackage      = "./cmd/swipt-driver"
	cliManifest        = "cli/package.json"
	driverEntitlements = "cmd/swipt-driver/swipt-driver.entitlements"
)

func main() {
	host := toolchain.Target{OS: runtime.GOOS, Arch: runtime.GOARCH}.Published()
	targets := flag.String("target", host, "comma-separated driver packages, such as darwin-arm64 or win32-x64, or all")
	flag.Usage = func() {
		fmt.Fprintln(flag.CommandLine.Output(), "usage: build_driver [-target TARGET,...|all]")
		fmt.Fprintln(flag.CommandLine.Output(), "Builds swipt-driver into build/driver/TARGET.")
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() > 0 {
		log.Fatal("unexpected arguments: ", flag.Args())
	}
	buildDirectory := toolchain.BuildDirectory()
	toolchain.PruneDownloads(buildDirectory)
	for _, t := range selectTargets(*targets) {
		executable := "swipt-driver"
		if t.OS == "windows" {
			executable += ".exe"
		}
		output := filepath.Join(buildDirectory, "driver", t.Published(), executable)
		command := exec.Command("go", "build", "-trimpath", "-ldflags", "-s -w -buildid=", "-o", output, driverPackage)
		command.Env = append(os.Environ(), toolchain.Environment(buildDirectory, t)...)
		run(command)
		if t.OS == "darwin" {
			// signDarwin lets JavaScriptCore compile scripts: without allow-jit, the one
			// of macOS 27.2 only interprets them, and a loop of a million additions takes
			// 6.4 ms instead of 0.3 ms.
			run(exec.Command("codesign", "--force", "--sign", "-", "--entitlements", driverEntitlements, output))
		}
	}
}

func selectTargets(list string) []toolchain.Target {
	targets := publishedTargets()
	if list == "all" {
		return targets
	}
	var selected []toolchain.Target
	for name := range strings.SplitSeq(list, ",") {
		index := slices.IndexFunc(targets, func(t toolchain.Target) bool { return t.Published() == name })
		if index < 0 {
			log.Fatal(name, " is not a driver package in ", cliManifest)
		}
		selected = append(selected, targets[index])
	}
	return selected
}

func publishedTargets() []toolchain.Target {
	content, err := os.ReadFile(cliManifest)
	if err != nil {
		log.Fatal(err)
	}
	var manifest struct {
		OptionalDependencies map[string]string `json:"optionalDependencies"`
	}
	if err = json.Unmarshal(content, &manifest); err != nil {
		log.Fatal(cliManifest, ": ", err)
	}
	var targets []toolchain.Target
	for name := range manifest.OptionalDependencies {
		t, ok := toolchain.ParseTarget(strings.TrimPrefix(name, "@swipt/driver-"))
		if !ok {
			log.Fatal(cliManifest, ": unexpected optional dependency ", name)
		}
		targets = append(targets, t)
	}
	slices.SortFunc(targets, func(a, b toolchain.Target) int {
		return strings.Compare(a.Published(), b.Published())
	})
	return targets
}

func run(command *exec.Cmd) {
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	if err := command.Run(); err != nil {
		log.Fatal(command.Args, ": ", err)
	}
}
