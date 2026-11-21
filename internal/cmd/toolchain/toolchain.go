package toolchain

import (
	"cmp"
	"log"
	"os"
	"os/exec"
	"path/filepath"
)

type Target struct {
	OS   string
	Arch string
}

var nodeNames = map[string]string{"windows": "win32", "amd64": "x64"}

func ParseTarget(published string) (Target, bool) {
	goNames := make(map[string]string, len(nodeNames))
	for goName, nodeName := range nodeNames {
		goNames[nodeName] = goName
	}
	for index := range len(published) {
		if published[index] == '-' {
			osName, arch := published[:index], published[index+1:]
			if osName == "" || arch == "" {
				return Target{}, false
			}
			return Target{OS: cmp.Or(goNames[osName], osName), Arch: cmp.Or(goNames[arch], arch)}, true
		}
	}
	return Target{}, false
}

func (t Target) String() string {
	return t.OS + "-" + t.Arch
}

func (t Target) Published() string {
	return cmp.Or(nodeNames[t.OS], t.OS) + "-" + cmp.Or(nodeNames[t.Arch], t.Arch)
}

func BuildDirectory() string {
	directory, err := filepath.Abs("build")
	if err != nil {
		log.Fatal(err)
	}
	return directory
}

func Environment(buildDirectory string, t Target) []string {
	environment := []string{"GOOS=" + t.OS, "GOARCH=" + t.Arch, "CGO_ENABLED=1"}
	switch t.OS {
	case "darwin":
		return append(environment, darwinEnvironment()...)
	case "linux":
		return append(environment, linuxEnvironment(buildDirectory, t)...)
	case "windows":
		return append(environment, windowsEnvironment(buildDirectory, t)...)
	case "android":
		return append(environment, androidEnvironment(buildDirectory, t)...)
	default:
		log.Fatal("unsupported target ", t.Published())
		return nil
	}
}

func run(command *exec.Cmd) {
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	if err := command.Run(); err != nil {
		log.Fatal(command.Args, ": ", err)
	}
}
