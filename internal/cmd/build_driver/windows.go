package main

import (
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// The names that MSVC, clang and xwin give each architecture.
type msvcArchitecture struct {
	library string
	triple  string
	xwin    string
}

var msvcArchitectures = map[string]msvcArchitecture{
	"amd64": {"x64", "x86_64-pc-windows-msvc", "x86_64"},
	"arm64": {"arm64", "aarch64-pc-windows-msvc", "aarch64"},
}

// The versions of the Windows SDK and the MSVC CRT that Bun's WebKit is built
// with, and the serviced UCRT that replaces the one these bring, which formats
// numbers wrong on ARM64 (oven-sh/bun 2596ac5, scripts/build/winsysroot.ts:44-60;
// scripts/build/ci-images/spec.ts:304-313).
var (
	windowsSDKVersion = "10.0.26100"
	msvcCRTVersion    = "14.44.17.14"
	ucrt              = map[string]download{
		"amd64": pin("https://api.nuget.org/v3-flatcontainer/microsoft.windows.sdk.cpp.x64/10.0.26100.8249/microsoft.windows.sdk.cpp.x64.10.0.26100.8249.nupkg", "dba411916afdd47c99fecdf7d5558df7679d291dc338746590ae9adf5fd0511b"),
		"arm64": pin("https://api.nuget.org/v3-flatcontainer/microsoft.windows.sdk.cpp.arm64/10.0.26100.8249/microsoft.windows.sdk.cpp.arm64.10.0.26100.8249.nupkg", "bae2af84725fb943385712a8a3e160d25be4ef0fdf7e5cccbd56c1daed7e36bf"),
	}
)

var visualStudioManifest = pin("https://download.visualstudio.microsoft.com/download/pr/6bd63d7f-1818-4e03-a552-9feb11496c19/8d9e516e27b5de40b0b3c5f042ea975a63c1744999a72125df4fbfc136b4ae9d/VisualStudio.17.Release.chman", "b300b5af6b2056b64861c3bb0329fe3356e24cd2c4b45e9d78a9a7ec123b1ed6")

// The go command before Go 1.27 passes -mthreads to the C compiler for windows,
// which clang rejects for an -msvc target (go1.26.8,
// src/cmd/go/internal/work/exec.go:2453).
const windowsToolchain = "go1.27.1"

func windowsEnvironment(t target) []string {
	architecture := msvcArchitectures[t.arch]
	sysroot := windowsSysroot(architecture)
	webKitDirectory := unpack(webKit[t], filepath.Join(buildDirectory, "webkit", t.String()), "--strip-components", "1", "--exclude", "bun-webkit/bin")
	sources := nativeSources()
	ucrtDirectory := filepath.Join(unpack(ucrt[t.arch], filepath.Join(buildDirectory, "deps", "ucrt-"+t.arch)), "ucrt", architecture.library)
	native := filepath.Join(buildDirectory, "native", t.String())
	if err := os.MkdirAll(native, 0o755); err != nil {
		log.Fatal(err)
	}
	environment := llvmEnvironment()
	compiler := []string{"/c", "/O2", "/MT", "-DNDEBUG", "--target=" + architecture.triple, "/winsysroot", sysroot, "-w"}
	archive(environment, filepath.Join(native, "mimalloc.lib"), compile(environment, "clang-cl", native, append(append(compiler, mimallocFlags...),
		"/TP", "-I"+filepath.Join(sources.mimalloc, "include"), filepath.Join(sources.mimalloc, "src/static.c"))...))
	archive(environment, filepath.Join(native, "zstd.lib"), compile(environment, "clang-cl", native, append(append(compiler, zstdFlags...), sources.zstdFiles()...)...))
	archive(environment, filepath.Join(native, "sqlite3.lib"), compile(environment, "clang-cl", native, append(append(compiler, sqliteFlags...), filepath.Join(sources.sqlite, "sqlite3.c"))...))
	msvc := filepath.Join(sysroot, "VC/Tools/MSVC", msvcCRTVersion)
	include := filepath.Join(sysroot, "Windows Kits/10/Include", windowsSDKVersion)
	library := filepath.Join(sysroot, "Windows Kits/10/Lib", windowsSDKVersion)
	clang := "clang --target=" + architecture.triple + " -fuse-ld=lld"
	return append(environment,
		"GOTOOLCHAIN="+windowsToolchain,
		"CC="+clang,
		"CXX="+strings.Replace(clang, "clang", "clang++", 1),
		"INCLUDE="+strings.Join([]string{filepath.Join(msvc, "include"), filepath.Join(include, "ucrt"), filepath.Join(include, "um"), filepath.Join(include, "shared"), filepath.Join(include, "winrt")}, ";"),
		"LIB="+strings.Join([]string{ucrtDirectory, filepath.Join(msvc, "lib", architecture.library), filepath.Join(library, "ucrt", architecture.library), filepath.Join(library, "um", architecture.library)}, ";"),
		"CGO_CFLAGS=-O2 -I"+filepath.Join(webKitDirectory, "include")+" -I"+filepath.Join(sources.zstd, "lib")+" -I"+sources.sqlite,
		"CGO_CXXFLAGS=-I"+filepath.Join(webKitDirectory, "include")+" -I"+filepath.Join(webKitDirectory, "include/JavaScriptCore"),
		"CGO_LDFLAGS=-L"+filepath.Join(webKitDirectory, "lib")+" -L"+native+" -Wl,-ignore:4099",
	)
}

func windowsSysroot(architecture msvcArchitecture) string {
	version, err := exec.Command("xwin", "--version").Output()
	if err != nil {
		log.Fatal("xwin: ", err)
	}
	arguments := []string{
		"--accept-license", "--manifest", fetch(visualStudioManifest), "--arch", architecture.xwin, "--sdk-version", windowsSDKVersion, "--crt-version", msvcCRTVersion,
		"--cache-dir", filepath.Join(buildDirectory, "xwin-cache"), "splat", "--use-winsysroot-style", "--preserve-ms-arch-notation",
	}
	return prepare(filepath.Join(buildDirectory, "winsysroot-"+architecture.xwin), digest(append([]string{string(version)}, arguments...)...), func(partial string) {
		run(exec.Command("xwin", append(arguments, "--output", partial)...))
	})
}
