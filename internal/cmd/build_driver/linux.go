package main

import (
	"io/fs"
	"log"
	"os"
	"path"
	"path/filepath"
	"strings"
)

var linuxTriples = map[string]string{"amd64": "x86_64-linux-gnu", "arm64": "aarch64-linux-gnu"}

// Bun's WebKit is built against glibc 2.31 and gcc 13's libstdc++ (oven-sh/bun
// v1.4.4, scripts/build/config.ts:591; scripts/build/ci-images/spec.ts:240-299).
var linuxSysroot = map[string][]download{
	"amd64": {
		pin("http://archive.ubuntu.com/ubuntu/pool/main/g/glibc/libc6_2.31-0ubuntu9.18_amd64.deb", "1b2281aac4935dfea1f89dfc19e445fdbb45303202af60679ccb8bf035f081a0"),
		pin("http://archive.ubuntu.com/ubuntu/pool/main/g/glibc/libc6-dev_2.31-0ubuntu9.18_amd64.deb", "11029900f2315b93ce487792ae1bf15f0927e5e50880c091e6a440b14d4e164a"),
		pin("http://archive.ubuntu.com/ubuntu/pool/main/l/linux/linux-libc-dev_5.4.0-216.236_amd64.deb", "70c9addd36aabd2d031d5ea0fc444f8b193027aef79ff3d8275d25800aed8ede"),
		pin("https://github.com/oven-sh/WebKit/releases/download/gcc-13-focal-debs/gcc-13-focal-amd64.tar.gz", "a2b3b6e10b175bbaaefeb3e9e703ca26a97ed6c1f19ca842d3e0b0c8f941e65b"),
	},
	"arm64": {
		pin("http://ports.ubuntu.com/ubuntu-ports/pool/main/g/glibc/libc6_2.31-0ubuntu9.18_arm64.deb", "4ff60d84ad78f3aa598297dc6966549fa6489f57599f197562ad91547f388da0"),
		pin("http://ports.ubuntu.com/ubuntu-ports/pool/main/g/glibc/libc6-dev_2.31-0ubuntu9.18_arm64.deb", "a690787a4ed5ad1bb9ce6aebe0bef3300e483e7ed97d11a6af3cdb107e642292"),
		pin("http://ports.ubuntu.com/ubuntu-ports/pool/main/l/linux/linux-libc-dev_5.4.0-216.236_arm64.deb", "9f487e7f3a9fff7f42f67172e139fd23c11b30e9b70bf6ae5a2e90b099547990"),
		pin("https://github.com/oven-sh/WebKit/releases/download/gcc-13-focal-debs/gcc-13-focal-arm64.tar.gz", "be19db90d94c52c6061280bbadcaad9b09db1e9f2e77a12f8c18c5425d2eb056"),
	},
}

var gccPackages = []string{"libgcc-13-dev_", "libstdc++-13-dev_", "libgcc-s1_"}

func linuxEnvironment(t target) []string {
	sysroot := linuxSysrootFor(t)
	webKitDirectory := unpack(webKit[t], filepath.Join(buildDirectory, "webkit", t.String()), "--strip-components", "1", "--exclude", "bun-webkit/bin")
	sources := nativeSources()
	compiler := []string{"-c", "-O2", "-fPIC", "--target=" + linuxTriples[t.arch], "--sysroot=" + sysroot}
	native := filepath.Join(buildDirectory, "native", t.String())
	if err := os.MkdirAll(native, 0o755); err != nil {
		log.Fatal(err)
	}
	environment := llvmEnvironment()
	archive(environment, filepath.Join(native, "libmimalloc.a"), compile(environment, "clang++", native, append(append(compiler, mimallocFlags...),
		"-fvisibility=hidden", "-Wno-deprecated", "-Wno-static-in-inline", "-ftls-model=initial-exec", "-DMI_DEFAULT_ALLOW_THP=0", "-x", "c++", "-I"+filepath.Join(sources.mimalloc, "include"), filepath.Join(sources.mimalloc, "src/static.c"))...))
	archive(environment, filepath.Join(native, "libzstd.a"), compile(environment, "clang", native, append(append(compiler, zstdFlags...), sources.zstdFiles()...)...))
	archive(environment, filepath.Join(native, "libsqlite3.a"), compile(environment, "clang", native, append(append(compiler, sqliteFlags...), filepath.Join(sources.sqlite, "sqlite3.c"))...))
	clang := "clang --target=" + linuxTriples[t.arch] + " --sysroot=" + sysroot
	return append(environment,
		"CC="+clang,
		"CXX="+strings.Replace(clang, "clang", "clang++", 1),
		"CGO_CFLAGS=-O2 -I"+filepath.Join(webKitDirectory, "include")+" -I"+filepath.Join(sources.zstd, "lib")+" -I"+sources.sqlite,
		"CGO_CXXFLAGS=-I"+filepath.Join(webKitDirectory, "include")+" -I"+filepath.Join(webKitDirectory, "include/JavaScriptCore"),
		"CGO_LDFLAGS=-fuse-ld=lld -L"+filepath.Join(webKitDirectory, "lib")+" -L"+native,
	)
}

func linuxSysrootFor(t target) string {
	inputs := append([]string(nil), gccPackages...)
	for _, source := range linuxSysroot[t.arch] {
		inputs = append(inputs, source.sha256)
	}
	return prepare(filepath.Join(buildDirectory, "sysroot", t.String()), digest(inputs...), func(sysroot string) {
		for _, source := range linuxSysroot[t.arch] {
			if strings.HasSuffix(source.url, ".deb") {
				debData(fetch(source), sysroot)
				continue
			}
			packages := unpack(source, filepath.Join(buildDirectory, "deps", strings.TrimSuffix(path.Base(source.url), ".tar.gz")))
			entries, err := os.ReadDir(packages)
			if err != nil {
				log.Fatal(err)
			}
			for _, entry := range entries {
				for _, prefix := range gccPackages {
					if strings.HasPrefix(entry.Name(), prefix) {
						debData(filepath.Join(packages, entry.Name()), sysroot)
					}
				}
			}
		}
		err := filepath.WalkDir(sysroot, func(file string, entry fs.DirEntry, err error) error {
			if err != nil || entry.Type()&fs.ModeSymlink == 0 {
				return err
			}
			destination, err := os.Readlink(file)
			if err != nil || !filepath.IsAbs(destination) {
				return err
			}
			relative, err := filepath.Rel(filepath.Dir(file), filepath.Join(sysroot, destination))
			if err != nil {
				return err
			}
			if err = os.Remove(file); err != nil {
				return err
			}
			return os.Symlink(relative, file)
		})
		if err != nil {
			log.Fatal(err)
		}
	})
}
