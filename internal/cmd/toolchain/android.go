package toolchain

import (
	"archive/zip"
	"log"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"strings"
)

var androidTriples = map[string]string{"arm64": "aarch64-linux-android"}

// The NDK that Bun's WebKit is built for Android with, and the NDK's runtimes
// that the host clang is given in place of its own (oven-sh/WebKit d6375c72,
// Dockerfile.android:6-8, 64-77). The API level is the minimum SDK of the app,
// android/app/build.gradle.kts.
var (
	androidNDK      = pin("https://dl.google.com/android/repository/android-ndk-r27c-linux.zip", "59c2f6dc96743b5daf5d1626684640b20a6bd2b1d85b13156b90333741bad5cc")
	androidAPILevel = 34
)

func androidEnvironment(buildDirectory string, t Target) []string {
	environment := llvmEnvironment()
	sysroot, resourceDirectory := androidSysroot(buildDirectory, environment, t)
	return crossEnvironment(buildDirectory, environment, t, "--target="+androidTriples[t.Arch]+strconv.Itoa(androidAPILevel), "--sysroot="+sysroot, "-resource-dir="+resourceDirectory)
}

func androidSysroot(buildDirectory string, environment []string, t Target) (sysroot string, resourceDirectory string) {
	output, err := exec.Command(lookPath(environment, "clang"), "-print-resource-dir").Output()
	if err != nil {
		log.Fatal("clang -print-resource-dir: ", err)
	}
	hostResourceDirectory := strings.TrimSpace(string(output))
	runtimeArch, _, _ := strings.Cut(androidTriples[t.Arch], "-")
	prebuilt := strings.TrimSuffix(path.Base(androidNDK.url), "-linux.zip") + "/toolchains/llvm/prebuilt/linux-x86_64/"
	runtimes := map[string]string{
		"libclang_rt.builtins-" + runtimeArch + "-android.a": "libclang_rt.builtins.a",
		runtimeArch + "/libunwind.a":                         "libunwind.a",
	}
	prepared := prepare(filepath.Join(buildDirectory, "sysroot", t.String()), digest(androidNDK.sha256, t.Arch, strconv.Itoa(androidAPILevel), hostResourceDirectory), func(partial string) {
		reader, err := zip.OpenReader(fetch(buildDirectory, androidNDK))
		if err != nil {
			log.Fatal(err)
		}
		defer reader.Close()
		runtimeDirectory := filepath.Join(partial, "clang", "lib", runtimeArch+"-unknown-linux-android"+strconv.Itoa(androidAPILevel))
		for _, entry := range reader.File {
			name, found := strings.CutPrefix(entry.Name, prebuilt)
			if !found {
				continue
			}
			if member, found := strings.CutPrefix(name, "sysroot/"); found {
				extract(entry, filepath.Join(partial, "sysroot", filepath.FromSlash(member)))
				continue
			}
			if _, runtime, found := strings.Cut(name, "/lib/linux/"); found && strings.HasPrefix(name, "lib/clang/") && runtimes[runtime] != "" {
				extract(entry, filepath.Join(runtimeDirectory, runtimes[runtime]))
			}
		}
		if err = os.Symlink(filepath.Join(hostResourceDirectory, "include"), filepath.Join(partial, "clang", "include")); err != nil {
			log.Fatal(err)
		}
	})
	return filepath.Join(prepared, "sysroot"), filepath.Join(prepared, "clang")
}
