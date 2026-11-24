package main

import (
	"archive/zip"
	"debug/buildinfo"
	"flag"
	"go/build"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"

	_ "github.com/sagernet/gomobile"
	"github.com/sagernet/sing/common"
	"github.com/swipt-automation/swipt/internal/cmd/toolchain"
)

const (
	gomobileModule    = "github.com/sagernet/gomobile"
	iosVersion        = "15.0"
	macOSVersion      = "14.0"
	androidAPI        = "34"
	outputPath        = "Frameworks/LibSwipt.xcframework"
	androidOutputPath = "android/app/libs/libswipt.aar"
	androidPackage    = "io.nekohasekai"
	androidProguard   = "-keep class go.** { *; }\n-keep class io.nekohasekai.libSwipt.** { *; }\n"
)

func main() {
	target := flag.String("target", "ios,iossimulator,macos", "gomobile bind target, or android")
	flag.Parse()
	binDirectory := filepath.Join(filepath.SplitList(build.Default.GOPATH)[0], "bin")
	gomobileVersion := pinnedGomobileVersion()
	var missingTools []string
	for _, tool := range []string{"gomobile", "gobind"} {
		if !toolInstalled(filepath.Join(binDirectory, tool), gomobileModule+"/cmd/"+tool, gomobileVersion) {
			missingTools = append(missingTools, gomobileModule+"/cmd/"+tool+"@"+gomobileVersion)
		}
	}
	if len(missingTools) > 0 {
		install := exec.Command("go", append([]string{"install", "-v"}, missingTools...)...)
		install.Env = append(os.Environ(), "GOBIN="+binDirectory)
		run(install)
	}

	output, platformArgs := outputPath, []string{"-target", *target, "-iosversion", iosVersion, "-macosversion", macOSVersion}
	ldflags := "-s -w -buildid= -checklinkname=0"
	env := os.Environ()
	android := *target == "android"
	if android {
		output, platformArgs = androidOutputPath, []string{"-target", "android/arm64", "-androidapi", androidAPI, "-javapkg", androidPackage}
		ldflags += " -extldflags=-Wl,-z,max-page-size=16384"
		buildDirectory := toolchain.BuildDirectory()
		toolchain.PruneDownloads(buildDirectory)
		env = append(env, toolchain.Environment(buildDirectory, toolchain.Target{OS: "android", Arch: "arm64"})...)
	}
	err := os.MkdirAll(filepath.Dir(output), 0o755)
	if err != nil {
		log.Fatal(err)
	}
	arguments := append([]string{
		"bind",
		"-v",
		"-trimpath",
		"-buildvcs=false",
		"-ldflags", ldflags,
		"-tags", "ts_omit_logtail,ts_omit_ssh,ts_omit_drive,ts_omit_taildrop,ts_omit_webclient,ts_omit_doctor,ts_omit_capture,ts_omit_kube,ts_omit_aws,ts_omit_synology,ts_omit_bird",
	}, append(platformArgs, "-o", output, "./libswipt")...)
	bind := exec.Command(filepath.Join(binDirectory, "gomobile"), arguments...)
	bind.Env = append(env, "PATH="+binDirectory+string(os.PathListSeparator)+pathValue(env))
	run(bind)
	if android {
		replaceProguardRules(output)
	}
}

func replaceProguardRules(path string) {
	reader, err := zip.OpenReader(path)
	if err != nil {
		log.Fatal(err)
	}
	temporaryPath := path + ".tmp"
	file, err := os.Create(temporaryPath)
	if err != nil {
		log.Fatal(err)
	}
	writer := zip.NewWriter(file)
	for _, entry := range reader.File {
		if entry.Name != "proguard.txt" {
			err = writer.Copy(entry)
			if err != nil {
				log.Fatal(err)
			}
			continue
		}
		rules, err := writer.Create(entry.Name)
		if err != nil {
			log.Fatal(err)
		}
		_, err = io.WriteString(rules, androidProguard)
		if err != nil {
			log.Fatal(err)
		}
	}
	err = writer.Close()
	if err != nil {
		log.Fatal(err)
	}
	err = file.Close()
	if err != nil {
		log.Fatal(err)
	}
	reader.Close()
	err = os.Rename(temporaryPath, path)
	if err != nil {
		log.Fatal(err)
	}
}

func pathValue(environment []string) string {
	value := os.Getenv("PATH")
	for _, entry := range environment {
		if len(entry) >= 5 && entry[:5] == "PATH=" {
			value = entry[5:]
		}
	}
	return value
}

func pinnedGomobileVersion() string {
	info, loaded := debug.ReadBuildInfo()
	if !loaded {
		log.Fatal("missing build info")
	}
	module := common.Find(info.Deps, func(module *debug.Module) bool {
		return module.Path == gomobileModule
	})
	if module == nil {
		log.Fatal("missing ", gomobileModule, " in build info")
	}
	return module.Version
}

func toolInstalled(path string, packagePath string, version string) bool {
	info, err := buildinfo.ReadFile(path)
	if err != nil {
		return false
	}
	return info.Path == packagePath && info.Main.Path == gomobileModule && info.Main.Version == version
}

func run(command *exec.Cmd) {
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	err := command.Run()
	if err != nil {
		log.Fatal(err)
	}
}
