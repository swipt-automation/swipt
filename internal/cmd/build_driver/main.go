package main

import (
	"cmp"
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
)

const (
	driverPackage = "./cmd/swipt-driver"
	cliManifest   = "cli/package.json"
)

var buildDirectory string

type target struct {
	os   string
	arch string
}

var nodeNames = map[string]string{"windows": "win32", "amd64": "x64"}

func (t target) String() string {
	return t.os + "-" + t.arch
}

func (t target) published() string {
	return cmp.Or(nodeNames[t.os], t.os) + "-" + cmp.Or(nodeNames[t.arch], t.arch)
}

func main() {
	host := target{runtime.GOOS, runtime.GOARCH}.published()
	targets := flag.String("target", host, "comma-separated driver packages, such as darwin-arm64 or win32-x64, or all")
	flag.Usage = func() {
		fmt.Fprintln(flag.CommandLine.Output(), "usage: build_driver [-target TARGET,...|all] [command [argument...]]")
		fmt.Fprintln(flag.CommandLine.Output(), "Builds swipt-driver into build/driver/TARGET, or runs command with the cgo environment of each target.")
		flag.PrintDefaults()
	}
	flag.Parse()
	var err error
	buildDirectory, err = filepath.Abs("build")
	if err != nil {
		log.Fatal(err)
	}
	pruneDownloads()
	for _, t := range selectTargets(*targets) {
		environment := []string{"GOOS=" + t.os, "GOARCH=" + t.arch, "CGO_ENABLED=1"}
		executable := "swipt-driver"
		switch t.os {
		case "darwin":
			environment = append(environment, darwinEnvironment()...)
		case "linux":
			environment = append(environment, linuxEnvironment(t)...)
		case "windows":
			environment = append(environment, windowsEnvironment(t)...)
			executable += ".exe"
		default:
			log.Fatal("unsupported target ", t.published())
		}
		command := exec.Command("go", "build", "-trimpath", "-ldflags", "-s -w -buildid=", "-o", filepath.Join(buildDirectory, "driver", t.published(), executable), driverPackage)
		if flag.NArg() > 0 {
			command = exec.Command(flag.Arg(0), flag.Args()[1:]...)
		}
		command.Env = append(os.Environ(), environment...)
		run(command)
	}
}

func selectTargets(list string) []target {
	targets := publishedTargets()
	if list == "all" {
		return targets
	}
	var selected []target
	for name := range strings.SplitSeq(list, ",") {
		index := slices.IndexFunc(targets, func(t target) bool { return t.published() == name })
		if index < 0 {
			log.Fatal(name, " is not a driver package in ", cliManifest)
		}
		selected = append(selected, targets[index])
	}
	return selected
}

func publishedTargets() []target {
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
	goNames := make(map[string]string)
	for goName, nodeName := range nodeNames {
		goNames[nodeName] = goName
	}
	var targets []target
	for name := range manifest.OptionalDependencies {
		platform, arch, found := strings.Cut(strings.TrimPrefix(name, "@swipt/driver-"), "-")
		if !found {
			log.Fatal(cliManifest, ": unexpected optional dependency ", name)
		}
		targets = append(targets, target{cmp.Or(goNames[platform], platform), cmp.Or(goNames[arch], arch)})
	}
	slices.SortFunc(targets, func(a, b target) int {
		return strings.Compare(a.published(), b.published())
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
