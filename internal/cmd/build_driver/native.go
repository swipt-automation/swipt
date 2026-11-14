package main

import (
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// The definitions Bun builds mimalloc with, apart from those that make it
// replace malloc (oven-sh/bun v1.4.4, scripts/build/deps/mimalloc.ts:30-125).
var mimallocFlags = []string{
	"-DMI_CMAKE_BUILD_TYPE=release", "-DMI_STATIC_LIB", "-DMI_SKIP_COLLECT_ON_EXIT=1", "-DMI_NO_PROCESS_DETACH=1",
	"-DMI_FREE_USE_PAGEMAP=1", "-DMI_BUILD_RELEASE",
}

var zstdFlags = []string{"-DZSTD_DISABLE_ASM=1"}

var sqliteFlags = []string{
	"-DSQLITE_ENABLE_COLUMN_METADATA", "-DSQLITE_ENABLE_DBSTAT_VTAB", "-DSQLITE_ENABLE_FTS3", "-DSQLITE_ENABLE_FTS3_PARENTHESIS",
	"-DSQLITE_ENABLE_FTS4", "-DSQLITE_ENABLE_FTS5", "-DSQLITE_ENABLE_MATH_FUNCTIONS", "-DSQLITE_ENABLE_PREUPDATE_HOOK",
	"-DSQLITE_ENABLE_RTREE", "-DSQLITE_ENABLE_SESSION", "-DSQLITE_OMIT_LOAD_EXTENSION",
}

type sources struct {
	mimalloc string
	zstd     string
	sqlite   string
}

func nativeSources() sources {
	deps := filepath.Join(buildDirectory, "deps")
	return sources{
		mimalloc: unpack(mimalloc, filepath.Join(deps, "mimalloc"), 1),
		zstd:     unpack(zstd, filepath.Join(deps, "zstd"), 1),
		sqlite:   unpack(sqlite, filepath.Join(deps, "sqlite"), 1),
	}
}

func (s sources) zstdFiles() []string {
	var files []string
	for _, directory := range []string{"lib/common", "lib/decompress"} {
		matched, err := filepath.Glob(filepath.Join(s.zstd, directory, "*.c"))
		if err != nil {
			log.Fatal(err)
		}
		files = append(files, matched...)
	}
	return files
}

const llvmVersion = "23"

func llvmEnvironment() []string {
	var directories []string
	for _, prefix := range []string{os.Getenv("LLVM_PREFIX"), "/opt/homebrew/opt/llvm@" + llvmVersion, "/opt/homebrew/opt/lld@" + llvmVersion, "/usr/local/opt/llvm@" + llvmVersion, "/usr/local/opt/lld@" + llvmVersion} {
		if prefix == "" {
			continue
		}
		if _, err := os.Stat(filepath.Join(prefix, "bin")); err == nil {
			directories = append(directories, filepath.Join(prefix, "bin"))
		}
	}
	if len(directories) == 0 {
		log.Fatal("LLVM ", llvmVersion, " is missing; install llvm@", llvmVersion, " and lld@", llvmVersion)
	}
	return []string{"PATH=" + strings.Join(append(directories, os.Getenv("PATH")), string(os.PathListSeparator))}
}

func compile(environment []string, compiler string, directory string, arguments ...string) []string {
	objects, err := os.MkdirTemp(directory, "objects-")
	if err != nil {
		log.Fatal(err)
	}
	command := exec.Command(lookPath(environment, compiler), arguments...)
	command.Dir = objects
	command.Env = append(os.Environ(), environment...)
	run(command)
	matched, err := filepath.Glob(filepath.Join(objects, "*"))
	if err != nil {
		log.Fatal(err)
	}
	return matched
}

func archive(environment []string, library string, objects []string) {
	if err := os.Remove(library); err != nil && !os.IsNotExist(err) {
		log.Fatal(err)
	}
	command := exec.Command(lookPath(environment, "llvm-ar"), append([]string{"rcs", library}, objects...)...)
	command.Env = append(os.Environ(), environment...)
	run(command)
	if err := os.RemoveAll(filepath.Dir(objects[0])); err != nil {
		log.Fatal(err)
	}
}

func lookPath(environment []string, name string) string {
	if filepath.IsAbs(name) {
		return name
	}
	for _, variable := range environment {
		value, found := strings.CutPrefix(variable, "PATH=")
		if !found {
			continue
		}
		for _, directory := range filepath.SplitList(value) {
			file := filepath.Join(directory, name)
			if info, err := os.Stat(file); err == nil && !info.IsDir() {
				return file
			}
		}
	}
	log.Fatal(name, " is missing; install llvm@", llvmVersion, " and lld@", llvmVersion)
	return ""
}
