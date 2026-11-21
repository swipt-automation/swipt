package main

import (
	"flag"
	"log"
	"os"
	"os/exec"
	"strings"

	"github.com/swipt-automation/swipt/internal/cmd/toolchain"
)

func main() {
	targets := flag.String("target", "", "comma-separated targets, such as linux-arm64,win32-arm64,android-arm64")
	flag.Parse()
	if *targets == "" || flag.NArg() == 0 {
		log.Fatal("usage: with_toolchain -target TARGET,... command [argument...]")
	}
	buildDirectory := toolchain.BuildDirectory()
	toolchain.PruneDownloads(buildDirectory)
	for name := range strings.SplitSeq(*targets, ",") {
		target, ok := toolchain.ParseTarget(name)
		if !ok {
			log.Fatal("invalid target ", name)
		}
		command := exec.Command(flag.Arg(0), flag.Args()[1:]...)
		command.Env = append(os.Environ(), toolchain.Environment(buildDirectory, target)...)
		command.Stdout = os.Stdout
		command.Stderr = os.Stderr
		if err := command.Run(); err != nil {
			log.Fatal(command.Args, ": ", err)
		}
	}
}
